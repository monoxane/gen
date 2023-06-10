package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

type page struct {
	Path          string
	OutPath       string
	Name          string
	Type          string
	Backlinks     map[string]string
	Content       template.HTML
	Navigation    template.HTML
	Footer        template.HTML
	StaticImports template.HTML
}

func (p *page) Render() {
	var result bytes.Buffer
	footerTemplate.Execute(&result, p)

	p.Footer = template.HTML(result.String())

	switch p.Type {
	case "HTML":
		renderHtml(*p)
	case "MD":
		renderMd(*p)
	}
}

var (
	mdTemplate     *template.Template
	footerTemplate *template.Template
	reHref         regexp.Regexp
	pages          map[string]*page = make(map[string]*page)
)

func NewPage(path, outPath, name string) (page, error) {
	p := page{
		Path:      path,
		OutPath:   outPath,
		Name:      name,
		Backlinks: make(map[string]string, 0),
	}

	navigationPartial, err := os.ReadFile("template/navigation.html")
	if err != nil {
		return page{}, fmt.Errorf("[gen/page/new] unable to open navigation partial: %s", err)
	}
	p.Navigation = template.HTML(navigationPartial)

	// footerPartial, err := os.ReadFile("template/footer.html")
	// if err != nil {
	// 	return page{}, fmt.Errorf("[gen/page/new] unable to open footer partial: %s", err)
	// }
	// p.Footer = template.HTML(footerPartial)

	staticImportPatials, err := os.ReadFile("template/static.html")
	if err != nil {
		return page{}, fmt.Errorf("[gen/page/new] unable to open static imports partial: %s", err)
	}
	p.StaticImports = template.HTML(staticImportPatials)

	return p, nil
}

func main() {
	reHref = *regexp.MustCompile(`<a\s+(?:[^>]*?\s+)?(?:href=")(\/.*?)(?:")`)

	var err error
	mdTemplate, err = template.ParseFiles("template/markdown.html")
	if err != nil {
		log.Printf("[gen/init/template] unable to open markdown template: %s", err)
		return
	} else {
		log.Printf("[gen/init/template] opened markdown template")
	}

	footerTemplate, err = template.ParseFiles("template/footer.html")
	if err != nil {
		log.Printf("[gen/init/template] unable to open footer template: %s", err)
		return
	} else {
		log.Printf("[gen/init/template] opened footer template")
	}

	parseDirectoryContent("content", "Oliver")

	log.Printf("[gen/parse] parsed %d pages", len(pages))

	for key, page := range pages {
		if page.Type != "" {
			log.Printf("[gen/parse/backlinks] parsing %s as %s", page.OutPath, key)
			links := reHref.FindAllStringSubmatch(string(page.Content), -1)
			for _, link := range links {
				log.Printf("[gen/parse/backlinks] found link in %s: %s", page.OutPath, link[1])
				p := fmt.Sprintf("public%s", link[1])
				if targetPage, ok := pages[p]; ok {
					targetPage.Backlinks[strings.Replace(page.OutPath, "public", "", 1)] = page.Name
				} else {
					p = fmt.Sprintf("public%s/index.html", link[1])
					if targetPage, ok := pages[p]; ok {
						targetPage.Backlinks[strings.Replace(page.OutPath, "public", "", 1)] = page.Name
					} else {
						log.Printf("[gen/parse/backlinks] unable to find page %s", p)
					}
				}
			}
		}
	}

	for _, page := range pages {
		page.Render()
	}
}

func parseDirectoryContent(directory, parent string) {
	inodes, err := os.ReadDir(directory)
	if err != nil {
		log.Fatal(err)
	}

	os.Mkdir("public", fs.FileMode(0700))
	for _, inode := range inodes {
		path := fmt.Sprintf("%s/%s", directory, inode.Name())
		outPath := strings.ToLower(path)
		outPath = strings.Replace(outPath, "content/", "", 1)
		outPath = strings.Replace(outPath, " ", "_", -1)
		outPath = strings.Replace(outPath, ".md", ".html", 1)
		outPath = fmt.Sprintf("public/%s", outPath)
		if inode.IsDir() {
			err := os.MkdirAll(outPath, 0700)
			if err != nil {
				log.Printf("[gen/process/dir] unable to create directory %s: %s", outPath, err)
				continue
			} else {
				log.Printf("[gen/process/dir] created directory %s", outPath)
			}

			childName := directory
			if childName == "content" {
				childName = parent
			}

			parseDirectoryContent(path, childName)

		} else {
			s, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[gen/parse/source] unable to read source %s: %s", outPath, err)
				continue
			}

			p, err := NewPage(path, outPath, strings.TrimSuffix(inode.Name(), filepath.Ext(inode.Name())))
			if err != nil {
				log.Print(err)
				continue
			}

			if p.Name == "index" {
				p.Name = parent
			}

			switch filepath.Ext(inode.Name()) {
			case ".html":
				p.Type = "HTML"

			case ".md":
				p.Content = markdown2html(s)
				p.Type = "MD"

			default:
				log.Printf("[gen/process/file] copying %s", path)
				copyFile(path, outPath)
			}

			pages[strings.Replace(p.OutPath, "/content", "", 1)] = &p
		}
	}
}

func markdown2html(md []byte) template.HTML {
	// create markdown parser with extensions
	extensions := parser.CommonExtensions | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(md)

	// create HTML renderer with extensions
	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return template.HTML(markdown.Render(doc, renderer))
}

func renderMd(p page) {
	f, err := os.Create(p.OutPath)
	if err != nil {
		log.Printf("[gen/render/file] unable to create file %s: %s", p.OutPath, err)
		return
	} else {
		err = mdTemplate.Execute(f, p)
		if err != nil {
			log.Printf("[gen/render/file] unable to render to file %s: %s", p.OutPath, err)
			return
		} else {
			log.Printf("[gen/render/file] rendered file %s", p.OutPath)
		}
	}
}

func renderHtml(p page) {
	source, err := template.ParseFiles(p.Path)
	if err != nil {
		log.Printf("[gen/render/dir] unable to open source file: %s", err)
		return
	}

	f, err := os.Create(p.OutPath)
	if err != nil {
		log.Printf("[gen/render/file] unable to create file %s: %s", p.OutPath, err)
		return
	} else {
		err = source.Execute(f, p)
		if err != nil {
			log.Printf("[gen/render/file] unable to render to file %s: %s", p.OutPath, err)
			return
		} else {
			log.Printf("[gen/render/file] rendered file %s", p.OutPath)
		}
	}
}

func copyFile(path, outPath string) {
	fin, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer fin.Close()

	fout, err := os.Create(outPath)
	if err != nil {
		log.Fatal(err)
	}
	defer fout.Close()

	_, err = io.Copy(fout, fin)

	if err != nil {
		log.Fatal(err)
	}
}
