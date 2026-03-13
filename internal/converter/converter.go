// Package converter handles HTML generation and PDF conversion for
// individual articles and digest collections.
package converter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/google/shlex"
)

const htmlTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
  body { font-family: sans-serif; font-size: 16px; line-height: 1.5; max-width: 800px; margin: 0 auto; padding: 2em; }
  img { max-width: 100%; height: auto; }
  h1 { font-size: 2em; border-bottom: 1px solid #ccc; padding-bottom: 0.5em; }
  pre { background: #f4f4f4; padding: 1em; overflow-x: auto; }
  blockquote { border-left: 4px solid #ccc; margin: 0; padding-left: 1em; color: #666; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
{{if .Byline}}<p><em>By {{.Byline}}</em></p>{{end}}
<hr>
{{.Content}}
</body>
</html>`

// PageData holds the template variables for rendering a single article.
type PageData struct {
	Title   string
	Content string
	Byline  string
}

const defaultCommandTimeout = 10 * time.Minute

// GenerateHTML renders an article to an HTML temp file and returns its path.
func GenerateHTML(title, content, byline string) (string, error) {
	tmpl, err := template.New("page").Parse(htmlTemplate)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "rss2rm-*.html")
	if err != nil {
		return "", err
	}
	defer f.Close()

	data := PageData{
		Title:   title,
		Content: content,
		Byline:  byline,
	}

	if err := tmpl.Execute(f, data); err != nil {
		return "", err
	}

	return f.Name(), nil
}

// HTMLToPDF converts an HTML file to PDF by executing commandTemplate,
// replacing {url} and {output} placeholders with the source and
// destination paths.
func HTMLToPDF(htmlPath, pdfPath, commandTemplate string) error {
	inputURL := "file://" + htmlPath
	inputURL = fmt.Sprintf("%q", inputURL)
	pdfPath = fmt.Sprintf("%q", pdfPath)

	cmdStr := strings.ReplaceAll(commandTemplate, "{url}", inputURL)
	cmdStr = strings.ReplaceAll(cmdStr, "{output}", pdfPath)

	parts, err := shlex.Split(cmdStr)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timed out after %s", defaultCommandTimeout)
	}
	if err != nil {
		return fmt.Errorf("command failed (output: %s): %w", string(output), err)
	}

	return nil
}

// DigestArticle represents a single article within a digest.
type DigestArticle struct {
	Title   string
	Byline  string
	Content string
	FeedName string
}

const digestHTMLTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
  body { font-family: sans-serif; font-size: 16px; line-height: 1.5; max-width: 800px; margin: 0 auto; padding: 2em; }
  img { max-width: 100%; height: auto; }
  h1 { font-size: 2em; border-bottom: 1px solid #ccc; padding-bottom: 0.5em; }
  h2 { font-size: 1.5em; margin-top: 2em; }
  pre { background: #f4f4f4; padding: 1em; overflow-x: auto; }
  blockquote { border-left: 4px solid #ccc; margin: 0; padding-left: 1em; color: #666; }
  .toc { margin: 1em 0 2em 0; }
  .toc ol { padding-left: 1.5em; }
  .toc li { margin: 0.3em 0; }
  .toc .feed-name { color: #888; font-size: 0.9em; }
  .article-separator { border: none; border-top: 2px solid #333; margin: 3em 0; }
  .article-meta { color: #666; font-size: 0.9em; margin-bottom: 1em; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Date}}</p>

<div class="toc">
<h2>Contents</h2>
<ol>
{{range $i, $a := .Articles}}<li><a href="#article-{{$i}}">{{$a.Title}}</a> <span class="feed-name">— {{$a.FeedName}}</span></li>
{{end}}</ol>
</div>

{{range $i, $a := .Articles}}<hr class="article-separator">
<h2 id="article-{{$i}}">{{$a.Title}}</h2>
<div class="article-meta">{{if $a.Byline}}By {{$a.Byline}} · {{end}}{{$a.FeedName}}</div>
{{$a.Content}}
{{end}}
</body>
</html>`

type digestData struct {
	Title    string
	Date     string
	Articles []DigestArticle
}

// GenerateDigestHTML produces a single HTML file combining multiple articles
// with a table of contents.
func GenerateDigestHTML(title string, articles []DigestArticle) (string, error) {
	tmpl, err := template.New("digest").Parse(digestHTMLTemplate)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "rss2rm-digest-*.html")
	if err != nil {
		return "", err
	}
	defer f.Close()

	data := digestData{
		Title:    title,
		Date:     time.Now().Format("January 2, 2006"),
		Articles: articles,
	}

	if err := tmpl.Execute(f, data); err != nil {
		return "", err
	}

	return f.Name(), nil
}
