package cli

import (
	"flag"
	"io"
)

const defaultInlineBodyBytes = 256 << 10

type runOptions struct {
	request, curl, caFile, cookieJar, proxy, outDir string
	inlineBytes                                     int
	help                                            bool
}

func newRunFlagSet(options *runOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("curlgo run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.request, "request", "", "Request JSON path or -")
	flags.StringVar(&options.curl, "curl", "", "Curl text path or -")
	flags.StringVar(&options.caFile, "ca-file", "", "CA PEM file")
	flags.StringVar(&options.cookieJar, "cookie-jar", "", "Cookie snapshot file")
	flags.StringVar(&options.proxy, "proxy", "", "Explicit proxy")
	flags.StringVar(&options.outDir, "out-dir", "", "Existing response body directory")
	flags.IntVar(&options.inlineBytes, "inline-body-bytes", defaultInlineBodyBytes, "Inline response body byte limit")
	flags.BoolVar(&options.help, "help", false, "Show machine-readable help")
	flags.BoolVar(&options.help, "h", false, "Show machine-readable help")
	return flags
}
