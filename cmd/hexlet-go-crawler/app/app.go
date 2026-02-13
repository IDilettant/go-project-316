package app

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/urfave/cli"

	"code/crawler"
	"code/internal/limiter"
)

// Run executes the CLI and writes the JSON report to stdout.
// If URL is missing, it prints help and returns nil.
func Run(args []string, stdout, stderr io.Writer, client *http.Client, clock limiter.Timer) error {
	app := cli.NewApp()
	app.Name = "hexlet-go-crawler"
	app.Usage = "analyze a website structure"
	app.UsageText = "hexlet-go-crawler [global options] command [command options] <url>"
	app.Writer = stdout
	app.ErrWriter = stderr
	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name:  "depth",
			Usage: "crawl depth",
			Value: 10,
		},
		cli.IntFlag{
			Name:  "retries",
			Usage: "number of retries for failed requests",
			Value: 1,
		},
		cli.DurationFlag{
			Name:  "delay",
			Usage: "delay between requests (example: 200ms, 1s)",
			Value: 0 * time.Millisecond,
		},
		cli.DurationFlag{
			Name:  "timeout",
			Usage: "per-request timeout",
			Value: 15 * time.Second,
		},
		cli.Float64Flag{
			Name:  "rps",
			Usage: "limit requests per second (overrides delay)",
		},
		cli.StringFlag{
			Name:  "user-agent",
			Usage: "custom user agent",
		},
		cli.IntFlag{
			Name:  "workers",
			Usage: "number of concurrent workers",
			Value: 4,
		},
	}
	app.Action = func(c *cli.Context) error {
		rootURL := c.Args().First()
		if rootURL == "" {
			_ = cli.ShowAppHelp(c)

			return nil
		}

		client.Timeout = c.Duration("timeout")
		options := optionsFromCLI(c, rootURL, client, clock)

		report, err := crawler.Analyze(context.Background(), options)
		if err != nil {
			return err
		}

		_, err = stdout.Write(report)
		if err != nil {
			return err
		}

		return nil
	}

	err := app.Run(args)
	if err != nil {
		return err
	}

	return nil
}

func optionsFromCLI(
	c *cli.Context,
	rootURL string,
	client *http.Client,
	clock limiter.Timer,
) crawler.Options {
	return crawler.Options{
		URL:         rootURL,
		Depth:       c.Int("depth"),
		IndentJSON:  true,
		Timeout:     c.Duration("timeout"),
		Delay:       c.Duration("delay"),
		RPS:         c.Float64("rps"),
		Retries:     c.Int("retries"),
		UserAgent:   c.String("user-agent"),
		Concurrency: c.Int("workers"),
		HTTPClient:  client,
		Clock:       clock,
	}
}
