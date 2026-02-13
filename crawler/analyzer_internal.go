package crawler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"code/internal/fetcher"
	"code/internal/limiter"
	"code/internal/parser"
	"code/internal/urlutil"

	"golang.org/x/sync/semaphore"
)

type crawlJob struct {
	url          string
	depth        int
	discoveredAt time.Time
	seq          uint64
}

type pageResult struct {
	job   crawlJob
	page  Page
	links []string
	err   error
}

type linkCheck struct {
	broken bool
	link   BrokenLink
	url    string
}

type linkCheckJob struct {
	idx      int
	url      string
	resultCh chan<- linkCheckResult
}

type linkCheckResult struct {
	idx   int
	check linkCheck
}

type fetchCacheEntry struct {
	result fetcher.Result
	err    error
	ready  chan struct{}
}

type assetCacheEntry struct {
	result assetFetchResult
	ready  chan struct{}
}

type assetFetchResult struct {
	statusCode int
	sizeBytes  int64
	err        string
}

type analyzer struct {
	options    Options
	baseURL    *url.URL
	fetch      *fetcher.Fetcher
	report     *Report
	maxDepth   int
	fetchSem   *semaphore.Weighted
	linkCheck  *linkChecker
	fetchMu    sync.Mutex
	fetchCache map[string]*fetchCacheEntry
	assetMu    sync.Mutex
	assetCache map[string]*assetCacheEntry
}

type crawlState struct {
	seen        map[string]bool
	analysisErr error
}

type aggregator struct {
	clock        limiter.Timer
	state        *crawlState
	jobs         chan crawlJob
	pending      int
	jobsClosed   bool
	maxDepth     int
	report       *Report
	baseURL      *url.URL
	nextSeq      uint64
	nextCommit   uint64
	pendingPages map[uint64]Page
}

type linkChecker struct {
	analyzer *analyzer
	jobs     chan linkCheckJob
	wg       sync.WaitGroup
}

func newLinkChecker(ctx context.Context, analyzer *analyzer, workerCount int) *linkChecker {
	jobs := make(chan linkCheckJob, workerCount*4)
	checker := &linkChecker{
		analyzer: analyzer,
		jobs:     jobs,
	}

	for range workerCount {
		checker.wg.Add(1)
		go func() {
			defer checker.wg.Done()

			for job := range jobs {
				brokenLink, broken := analyzer.checkBrokenLink(ctx, job.url)
				job.resultCh <- linkCheckResult{
					idx: job.idx,
					check: linkCheck{
						broken: broken,
						link:   brokenLink,
						url:    job.url,
					},
				}
			}
		}()
	}

	return checker
}

func (c *linkChecker) stop() {
	close(c.jobs)
	c.wg.Wait()
}

func newAnalyzer(options Options, baseURL *url.URL, fetch *fetcher.Fetcher, report *Report) *analyzer {
	maxConcurrentFetch := normalizeMaxConcurrentFetch(options)

	return &analyzer{
		options:    options,
		baseURL:    baseURL,
		fetch:      fetch,
		report:     report,
		maxDepth:   normalizeMaxDepth(options.Depth),
		fetchSem:   semaphore.NewWeighted(int64(maxConcurrentFetch)),
		fetchCache: map[string]*fetchCacheEntry{},
		assetCache: map[string]*assetCacheEntry{},
	}
}

func (a *analyzer) run(ctx context.Context) error {
	workerCount := a.options.Concurrency
	if workerCount < 1 {
		workerCount = 1
	}

	linkCheckWorkers := linkCheckPoolSize(a.options)

	a.linkCheck = newLinkChecker(ctx, a, linkCheckWorkers)
	defer a.linkCheck.stop()

	jobBuffer := workerCount * 4
	if jobBuffer < 16 {
		jobBuffer = 16
	}

	jobs := make(chan crawlJob, jobBuffer)
	results := make(chan pageResult, workerCount)

	var workersWG sync.WaitGroup
	for range workerCount {
		workersWG.Add(1)

		go func() {
			defer workersWG.Done()
			a.worker(ctx, jobs, results)
		}()
	}

	go func() {
		workersWG.Wait()
		close(results)
	}()

	state := &crawlState{
		seen: map[string]bool{},
	}

	agg := &aggregator{
		clock:        a.options.Clock,
		state:        state,
		jobs:         jobs,
		maxDepth:     a.maxDepth,
		report:       a.report,
		baseURL:      a.baseURL,
		pendingPages: make(map[uint64]Page),
	}

	agg.enqueue(ctx, crawlJob{
		url:          a.baseURL.String(),
		depth:        0,
		discoveredAt: a.options.Clock.Now(),
	})
	agg.closeJobsIfNeeded()

	return a.drainResults(ctx, agg, results)
}

func (a *analyzer) acquireFetch(ctx context.Context) bool {
	return a.fetchSem.Acquire(ctx, 1) == nil
}

func (a *analyzer) releaseFetch() {
	a.fetchSem.Release(1)
}

func (a *analyzer) drainResults(
	ctx context.Context,
	agg *aggregator,
	results <-chan pageResult,
) error {
	canceled := false
	for {
		if !canceled {
			select {
			case result, ok := <-results:
				if !ok {
					return agg.state.analysisErr
				}
				agg.onResult(ctx, result)
			case <-ctx.Done():
				canceled = true
				agg.closeJobsIfNeeded()
			}

			continue
		}

		result, ok := <-results
		if !ok {
			return agg.state.analysisErr
		}

		agg.onResult(ctx, result)
	}
}

func (a *analyzer) worker(ctx context.Context, jobs <-chan crawlJob, results chan<- pageResult) {
	for job := range jobs {
		result := a.processJob(ctx, job)
		results <- result
	}
}

func (a *aggregator) enqueue(ctx context.Context, job crawlJob) {
	if a.state.seen[job.url] {
		return
	}

	jobWithSeq := job
	jobWithSeq.seq = a.nextSeq

	select {
	case a.jobs <- jobWithSeq:
		a.state.seen[job.url] = true
		a.nextSeq++
		a.pending++
	case <-ctx.Done():
	}
}

func (a *aggregator) closeJobsIfNeeded() {
	if a.pending != 0 || a.jobsClosed {
		return
	}

	close(a.jobs)
	a.jobsClosed = true
}

func (a *aggregator) onResult(ctx context.Context, result pageResult) {
	a.pending--
	a.handleResult(ctx, result)
	a.closeJobsIfNeeded()
}

func (a *aggregator) handleResult(ctx context.Context, result pageResult) {
	a.pendingPages[result.job.seq] = result.page
	a.flushCommitted()

	if result.job.depth == 0 && result.err != nil && a.state.analysisErr == nil {
		a.state.analysisErr = result.err
	}

	nextDepth := result.job.depth + 1
	if nextDepth > a.maxDepth {
		return
	}

	for _, link := range result.links {
		if !urlutil.SameOrigin(a.baseURL, link) {
			continue
		}

		a.enqueue(ctx, crawlJob{
			url:          link,
			depth:        nextDepth,
			discoveredAt: a.clock.Now(),
		})
	}
}

func (a *aggregator) flushCommitted() {
	for {
		page, ok := a.pendingPages[a.nextCommit]
		if !ok {
			return
		}

		a.report.Pages = append(a.report.Pages, page)
		delete(a.pendingPages, a.nextCommit)
		a.nextCommit++
	}
}

func (a *analyzer) processJob(ctx context.Context, job crawlJob) pageResult {
	page := newPage(job.url, job.depth, job.discoveredAt)
	result, err := a.fetchWithCache(ctx, job.url)
	page.HTTPStatus = result.StatusCode

	if err != nil || result.StatusCode >= http.StatusBadRequest {
		page.Status = statusError
		page.Error = errorString(err, result.StatusCode)
		page.BrokenLinks = nil
		page.Assets = nil

		return pageResult{
			job:  job,
			page: page,
			err:  errorForStatus(err, result.StatusCode),
		}
	}

	parsed, parseErr := parser.ParseHTML(result.Body)
	if parseErr != nil {
		page.Status = statusError
		page.Error = fmt.Sprintf("parse html: %v", parseErr)
		page.BrokenLinks = nil
		page.Assets = nil

		return pageResult{
			job:  job,
			page: page,
			err:  fmt.Errorf("parse html: %w", parseErr),
		}
	}

	page.Status = statusOK
	page.SEO = SEO{
		HasTitle:       parsed.SEO.HasTitle,
		Title:          parsed.SEO.Title,
		HasDescription: parsed.SEO.HasDescription,
		Description:    parsed.SEO.Description,
		HasH1:          parsed.SEO.HasH1,
	}

	brokenLinks, pageLinks := a.checkLinks(ctx, job, parsed.Links)
	page.BrokenLinks = dedupBrokenLinks(brokenLinks)
	page.Assets = a.collectAssets(ctx, job.url, parsed.Assets)

	return pageResult{
		job:   job,
		page:  page,
		links: pageLinks,
	}
}

func (a *analyzer) checkLinks(ctx context.Context, job crawlJob, links []string) ([]BrokenLink, []string) {
	resolved := a.resolveLinks(job.url, links)
	if len(resolved) == 0 {
		return []BrokenLink{}, []string{}
	}

	results, processed := a.runLinkChecks(ctx, resolved)

	return buildLinkResults(results, processed)
}

func (a *analyzer) runLinkChecks(ctx context.Context, resolved []string) ([]linkCheck, []bool) {
	results := make([]linkCheck, len(resolved))
	processed := make([]bool, len(resolved))

	if len(resolved) == 0 {
		return results, processed
	}

	resultCh := make(chan linkCheckResult, len(resolved))
	sent := 0
feedLoop:
	for idx, absoluteURL := range resolved {
		select {
		case <-ctx.Done():
			break feedLoop
		case a.linkCheck.jobs <- linkCheckJob{
			idx:      idx,
			url:      absoluteURL,
			resultCh: resultCh,
		}:
			sent++
		}
	}

	for range sent {
		result := <-resultCh
		results[result.idx] = result.check
		processed[result.idx] = true
	}

	return results, processed
}

func normalizeMaxConcurrentFetch(opts Options) int {
	maxConcurrentFetch := opts.MaxConcurrentFetch

	if maxConcurrentFetch <= 0 {
		maxConcurrentFetch = opts.Concurrency
	}

	if maxConcurrentFetch < 1 {
		maxConcurrentFetch = 1
	}

	return maxConcurrentFetch
}

func linkCheckPoolSize(opts Options) int {
	maxConcurrentFetch := normalizeMaxConcurrentFetch(opts)
	workerCount := 2

	if workerCount > maxConcurrentFetch {
		workerCount = maxConcurrentFetch
	}

	if workerCount < 1 {
		workerCount = 1
	}

	return workerCount
}

func buildLinkResults(results []linkCheck, processed []bool) ([]BrokenLink, []string) {
	if len(processed) > len(results) {
		processed = processed[:len(results)]
	}

	brokenLinks := make([]BrokenLink, 0, len(results))
	crawlLinks := make([]string, 0, len(results))
	seenBroken := map[string]bool{}

	for idx, res := range results[:len(processed)] {
		if !processed[idx] {
			continue
		}

		if res.broken {
			key := res.url
			if key == "" {
				key = res.link.URL
			}
			key = canonicalBrokenURL(key)

			if seenBroken[key] {
				continue
			}

			seenBroken[key] = true

			broken := res.link
			broken.URL = key
			brokenLinks = append(brokenLinks, broken)

			continue
		}

		crawlLinks = append(crawlLinks, res.url)
	}

	return brokenLinks, crawlLinks
}

func dedupBrokenLinks(links []BrokenLink) []BrokenLink {
	if len(links) < 2 {
		if len(links) == 1 {
			out := links[0]
			out.URL = canonicalBrokenURL(out.URL)
			return []BrokenLink{out}
		}

		return links
	}

	unique := make([]BrokenLink, 0, len(links))
	seen := make(map[string]bool, len(links))

	for _, link := range links {
		key := canonicalBrokenURL(link.URL)
		if seen[key] {
			continue
		}

		seen[key] = true
		out := link
		out.URL = key
		unique = append(unique, out)
	}

	return unique
}

func canonicalBrokenURL(raw string) string {
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)

	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()

	switch {
	case parsed.Scheme == "http" && port == "80":
		port = ""
	case parsed.Scheme == "https" && port == "443":
		port = ""
	}

	if port == "" {
		parsed.Host = host
	} else {
		parsed.Host = net.JoinHostPort(host, port)
	}

	if parsed.Path == "/" {
		parsed.Path = ""
	}

	parsed.RawPath = ""
	if parsed.RawQuery == "" {
		parsed.ForceQuery = false
	}

	return parsed.String()
}

func (a *analyzer) resolveLinks(pageURL string, links []string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	resolved := make([]string, 0, len(links))
	seen := map[string]bool{}

	for _, link := range links {
		absoluteURL, ok := urlutil.Resolve(base, link)
		if !ok {
			continue
		}

		if seen[absoluteURL] {
			continue
		}

		seen[absoluteURL] = true
		resolved = append(resolved, absoluteURL)
	}

	return resolved
}

func (a *analyzer) checkBrokenLink(ctx context.Context, absoluteURL string) (BrokenLink, bool) {
	result, err := a.fetchWithCache(ctx, absoluteURL)

	broken := err != nil || result.StatusCode >= http.StatusBadRequest
	if !broken {
		return BrokenLink{}, false
	}

	return BrokenLink{
		URL:        absoluteURL,
		StatusCode: result.StatusCode,
		Error:      errorString(err, result.StatusCode),
	}, true
}

func (a *analyzer) fetchWithCache(ctx context.Context, absoluteURL string) (fetcher.Result, error) {
	a.fetchMu.Lock()

	if cached, ok := a.fetchCache[absoluteURL]; ok {
		ready := cached.ready
		a.fetchMu.Unlock()

		select {
		case <-ready:
			return cached.result, cached.err
		case <-ctx.Done():
			return fetcher.Result{}, ctx.Err()
		}
	}

	entry := &fetchCacheEntry{ready: make(chan struct{})}
	a.fetchCache[absoluteURL] = entry
	a.fetchMu.Unlock()

	if !a.acquireFetch(ctx) {
		a.fetchMu.Lock()
		delete(a.fetchCache, absoluteURL)
		a.fetchMu.Unlock()

		entry.result = fetcher.Result{}
		entry.err = ctx.Err()
		close(entry.ready)

		return entry.result, entry.err
	}
	defer a.releaseFetch()

	result, err := a.fetch.Fetch(ctx, absoluteURL)
	entry.result = result
	entry.err = err
	close(entry.ready)

	return result, err
}

func (a *analyzer) collectAssets(ctx context.Context, pageURL string, assets []parser.AssetRef) []Asset {
	resolved := []Asset{}
	seen := map[string]bool{}

	base, err := url.Parse(pageURL)
	if err != nil {
		return resolved
	}

	for _, assetRef := range assets {
		absoluteURL, ok := urlutil.Resolve(base, assetRef.URL)
		if !ok {
			continue
		}

		if seen[absoluteURL] {
			continue
		}

		seen[absoluteURL] = true

		asset := a.getAsset(ctx, absoluteURL, assetRef.Type)
		resolved = append(resolved, asset)
	}

	return resolved
}

func (a *analyzer) getAsset(ctx context.Context, absoluteURL string, assetType string) Asset {
	a.assetMu.Lock()
	if cached, ok := a.assetCache[absoluteURL]; ok {
		ready := cached.ready
		a.assetMu.Unlock()

		select {
		case <-ready:
			return buildAssetFromResult(absoluteURL, assetType, cached.result)
		case <-ctx.Done():
			return Asset{
				URL:        absoluteURL,
				Type:       assetType,
				StatusCode: 0,
				SizeBytes:  0,
				Error:      ctx.Err().Error(),
			}
		}
	}

	entry := &assetCacheEntry{ready: make(chan struct{})}
	a.assetCache[absoluteURL] = entry
	a.assetMu.Unlock()

	if !a.acquireFetch(ctx) {
		a.assetMu.Lock()
		delete(a.assetCache, absoluteURL)
		a.assetMu.Unlock()

		entry.result = assetFetchResult{
			statusCode: 0,
			sizeBytes:  0,
			err:        ctx.Err().Error(),
		}

		close(entry.ready)

		return buildAssetFromResult(absoluteURL, assetType, entry.result)
	}
	defer a.releaseFetch()

	entry.result = fetchAssetResult(ctx, a.fetch, absoluteURL)
	close(entry.ready)

	return buildAssetFromResult(absoluteURL, assetType, entry.result)
}

func buildAssetFromResult(absoluteURL string, assetType string, result assetFetchResult) Asset {
	return Asset{
		URL:        absoluteURL,
		Type:       assetType,
		StatusCode: result.statusCode,
		SizeBytes:  result.sizeBytes,
		Error:      result.err,
	}
}

func normalizeMaxDepth(depth int) int {
	if depth < 0 {
		return 0
	}

	return depth
}
