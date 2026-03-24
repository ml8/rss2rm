# PDF Generation Options for rss2rm

Researched 2026-03-24. Evaluated alternatives to the original
pandoc + weasyprint pipeline. **Decision: dropped pandoc, kept weasyprint.**

## Current Approach

rss2rm uses **weasyprint** directly via a configurable shell command:

```
weasyprint {url} {output}
```

Previously, pandoc was used as a wrapper (`pandoc {url} -o {output}
--pdf-engine=weasyprint`), but pandoc added no value — it just passed
HTML through to weasyprint. Removing it saves ~100MB in Docker images
and eliminates one dependency.

## Alternatives

### 1. WeasyPrint directly (drop pandoc)

Call `weasyprint input.html output.pdf` without the pandoc middleman.

- **Pros**: Removes pandoc (~100MB). Simpler pipeline. Same output.
- **Cons**: Still requires Python + weasyprint.
- **Effort**: Trivial — change one string constant.
- **Link**: https://weasyprint.org/

### 2. chromedp (headless Chrome from Go)

Use the Go `chromedp` library to control headless Chrome for PDF output.

- **Pros**: Pure Go library. Full CSS3/JS support. Pixel-perfect output.
- **Cons**: Requires Chrome binary (~300MB). High memory use. Slow starts.
- **Effort**: Medium — ~50 lines of Go to replace `HTMLToPDF`.
- **Link**: https://github.com/chromedp/chromedp

### 3. Rod (headless Chrome, higher-level API)

Similar to chromedp with a cleaner API and auto-download of Chrome.

- **Pros**: Cleaner API. Same rendering quality as chromedp.
- **Cons**: Same resource costs. Auto-download may not suit containers.
- **Effort**: Medium.
- **Link**: https://github.com/nicedoc/go-rod

### 4. gpdf (pure Go, zero dependencies)

New pure-Go PDF library with a builder API. No HTML input.

- **Pros**: Zero deps. No CGO. No binaries. Fast. Single binary deploy.
- **Cons**: Does not accept HTML/CSS. Would require replacing the entire
  rendering pipeline. Article content from readability is HTML — no
  HTML-to-gpdf-builder converter exists. New and unproven.
- **Effort**: Very high. Fundamental architecture change.
- **Link**: https://github.com/gpdf-dev/gpdf

### 5. go-wkhtmltopdf

Go wrapper for wkhtmltopdf (WebKit-based).

- **Pros**: Battle-tested. Good CSS. Go wrapper API.
- **Cons**: wkhtmltopdf is unmaintained/archived. Old WebKit. No future.
- **Effort**: Low.
- **Link**: https://github.com/SebastiaanKlippert/go-wkhtmltopdf

### 6. gofpdf / gopdf / maroto

Pure Go PDF libraries with programmatic APIs.

- **Pros**: Pure Go. No dependencies.
- **Cons**: No HTML input. Manual layout only. gofpdf is archived.
- **Effort**: Very high — same problem as gpdf.

### 7. unidoc/unipdf (commercial)

Commercial Go PDF library.

- **Pros**: Feature-rich. Pure Go. Professional support.
- **Cons**: Commercial license. No HTML input. Overkill.
- **Effort**: Very high + licensing cost.
- **Link**: https://unidoc.io/

## Comparison

| Option | HTML Input | Pure Go | No External Binary | Docker Size | Maintained |
|---|---|---|---|---|---|
| pandoc+weasyprint (current) | Yes | No | No | +200MB | Yes |
| weasyprint only | Yes | No | No | +100MB | Yes |
| chromedp | Yes | Yes | No (Chrome) | +300MB | Yes |
| gpdf | No | Yes | Yes | Tiny | New |
| go-wkhtmltopdf | Yes | No | No | +100MB | Dead |
| gofpdf/gopdf | No | Yes | Yes | Tiny | Mixed |

## Recommendation

**Drop pandoc, call weasyprint directly.** One-line change:

```
Before: "pandoc {url} -o {output} --pdf-engine=weasyprint"
After:  "weasyprint {url} {output}"
```

Saves ~100MB in Docker, removes one dependency, same output quality.

**Do not switch to gpdf or other programmatic PDF libraries.** They
cannot render arbitrary article HTML. They are for invoices and reports
with known layouts.

**chromedp is viable but not worth it** for this use case. WeasyPrint
handles CSS3 well enough for article content. Chrome adds 300MB.

**Long-term**: If a pure-Go HTML/CSS rendering engine appears, revisit.
None exists today.
