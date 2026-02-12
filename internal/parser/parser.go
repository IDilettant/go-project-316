package parser

import (
	"bytes"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// SEOData represents extracted SEO information.
type SEOData struct {
	HasTitle       bool
	Title          string
	HasDescription bool
	Description    string
	HasH1          bool
}

// AssetRef describes an asset reference in HTML.
type AssetRef struct {
	URL  string
	Type string
}

// ParseResult aggregates HTML analysis results.
type ParseResult struct {
	Links  []string
	SEO    SEOData
	Assets []AssetRef
}

// ParseHTML parses HTML and extracts links, SEO, and assets.
// Missing SEO elements yield false flags and empty strings; text is HTML-decoded.
func ParseHTML(body []byte) (ParseResult, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ParseResult{}, err
	}

	return ParseResult{
		Links:  parseLinks(doc),
		SEO:    parseSEO(doc),
		Assets: parseAssets(doc),
	}, nil
}

func parseSEO(doc *goquery.Document) SEOData {
	seo := SEOData{}

	titleSelection := doc.Find("title").First()
	seo.HasTitle = titleSelection.Length() > 0
	if seo.HasTitle {
		seo.Title = cleanHumanText(titleSelection.Text())
	}

	hasDescription, description := findMetaDescription(doc)
	seo.HasDescription = hasDescription
	seo.Description = description

	seo.HasH1 = doc.Find("h1").Length() > 0

	return seo
}

func findMetaDescription(doc *goquery.Document) (bool, string) {
	var (
		found       bool
		description string
	)

	doc.Find("meta[name]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		name, ok := selection.Attr("name")
		if !ok {
			return true
		}

		if !strings.EqualFold(strings.TrimSpace(name), "description") {
			return true
		}

		found = true
		content, _ := selection.Attr("content")
		description = cleanHumanText(content)

		return false
	})

	return found, description
}

func parseLinks(doc *goquery.Document) []string {
	links := []string{}
	doc.Find("a[href]").Each(func(_ int, selection *goquery.Selection) {
		href, ok := selection.Attr("href")
		if !ok {
			return
		}

		links = append(links, strings.TrimSpace(href))
	})

	return links
}

func parseAssets(doc *goquery.Document) []AssetRef {
	assets := []AssetRef{}
	assets = append(assets, parseAssetBySelector(doc, "img[src]", "src", "image")...)
	assets = append(assets, parseAssetBySelector(doc, "script[src]", "src", "script")...)
	assets = append(assets, parseStylesheets(doc)...)

	return assets
}

func parseAssetBySelector(doc *goquery.Document, selector string, attr string, assetType string) []AssetRef {
	assets := []AssetRef{}
	doc.Find(selector).Each(func(_ int, selection *goquery.Selection) {
		value, ok := selection.Attr(attr)
		if !ok {
			return
		}

		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}

		assets = append(assets, AssetRef{URL: trimmed, Type: assetType})
	})

	return assets
}

func parseStylesheets(doc *goquery.Document) []AssetRef {
	assets := []AssetRef{}
	doc.Find("link[href]").Each(func(_ int, selection *goquery.Selection) {
		relValue, ok := selection.Attr("rel")
		if !ok {
			return
		}

		if !strings.Contains(strings.ToLower(relValue), "stylesheet") {
			return
		}

		href, ok := selection.Attr("href")
		if !ok {
			return
		}

		trimmed := strings.TrimSpace(href)
		if trimmed == "" {
			return
		}

		assets = append(assets, AssetRef{URL: trimmed, Type: "style"})
	})

	return assets
}
