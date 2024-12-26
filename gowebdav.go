package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/context"
	"golang.org/x/net/webdav"
)

var (
	flagRootDir    = flag.String("dir", "", "webdav root dir")
	flagHttpAddr   = flag.String("port", "5005", "http or https port")
	flagHttpsMode  = flag.Bool("https-mode", false, "use https mode")
	flagCertFile   = flag.String("https-cert-file", "cert.pem", "https cert file")
	flagKeyFile    = flag.String("https-key-file", "key.pem", "https key file")
	flagUserName   = flag.String("user", "", "user name")
	flagPassword   = flag.String("password", "", "user password")
	flagReadonly   = flag.Bool("read-only", false, "read only")
	flagShowHidden = flag.Bool("show-hidden", false, "show hidden files")
)

func init() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of WebDAV Server\n")
		flag.PrintDefaults()
	}
}

type SkipBrokenLink struct {
	webdav.Dir
}

func (d SkipBrokenLink) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	fileinfo, err := d.Dir.Stat(ctx, name)
	if err != nil && os.IsNotExist(err) {
		return nil, filepath.SkipDir
	}
	return fileinfo, err
}

func main() {
	if *flagRootDir == "" || *flagHttpAddr == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "\nError: -dir and -http flags are required.")
		os.Exit(0)
	}

	httpAddress := *flagHttpAddr
	if !strings.Contains(httpAddress, ":") {
		httpAddress = ":" + httpAddress
	}

	fs := &webdav.Handler{
		FileSystem: SkipBrokenLink{webdav.Dir(*flagRootDir)},
		LockSystem: webdav.NewMemLS(),
	}
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if !authenticate(w, req) {
			return
		}
		if req.Method == "GET" && handleDirList(fs.FileSystem, w, req) {
			return
		}
		if *flagReadonly && isWriteMethod(req.Method) {
			http.Error(w, "WebDAV: Read Only!!!", http.StatusForbidden)
			return
		}
		fs.ServeHTTP(w, req)
	})

	startServer(httpAddress)
}

func authenticate(w http.ResponseWriter, req *http.Request) bool {
	if *flagUserName != "" && *flagPassword != "" {
		username, password, ok := req.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		if username != *flagUserName || password != *flagPassword {
			http.Error(w, "WebDAV: need authorized!", http.StatusUnauthorized)
			return false
		}
	}
	return true
}

func isWriteMethod(method string) bool {
	switch method {
	case "PUT", "DELETE", "PROPPATCH", "MKCOL", "COPY", "MOVE":
		return true
	default:
		return false
	}
}

func startServer(httpAddress string) {
	var err error
	if *flagHttpsMode {
		err = http.ListenAndServeTLS(httpAddress, *flagCertFile, *flagKeyFile, nil)
	} else {
		err = http.ListenAndServe(httpAddress, nil)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

func handleDirList(fs webdav.FileSystem, w http.ResponseWriter, req *http.Request) bool {
	ctx := context.Background()
	f, err := fs.OpenFile(ctx, req.URL.Path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if fi, _ := f.Stat(); fi != nil && !fi.IsDir() {
		return false
	}
	if !strings.HasSuffix(req.URL.Path, "/") {
		http.Redirect(w, req, req.URL.Path+"/", http.StatusFound)
		return true
	}
	dirs, err := f.Readdir(-1)
	if err != nil {
		log.Print(w, "Error reading directory", http.StatusInternalServerError)
		return false
	}

	sortDirs(dirs)

	folderName := filepath.Base(req.URL.Path)
	nav := generateNavLinks(req.URL.Path)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "%s", generateHTML(folderName, nav, dirs))
	return true
}

func sortDirs(dirs []os.FileInfo) {
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].IsDir() && !dirs[j].IsDir() {
			return true
		}
		if !dirs[i].IsDir() && dirs[j].IsDir() {
			return false
		}
		return dirs[i].Name() < dirs[j].Name()
	})
}

func generateNavLinks(currentDir string) string {
	parts := strings.Split(currentDir, "/")
	var navLinks []string
	for i := 1; i < len(parts); i++ {
		navPath := "/" + strings.Join(parts[1:i+1], "/")
		navLinks = append(navLinks, fmt.Sprintf(`<a href="%s">%s</a>`, navPath, parts[i]))
	}
	return fmt.Sprintf(`
	<header>
	<div class="wrapper"><div class="breadcrumbs">Folder Path</div>
			<h1>
			<a href="/">/</a>%s
			</h1>
		</div>
	</header>
	`, strings.Join(navLinks, " / "))
}

func generateHTML(folderName, nav string, dirs []os.FileInfo) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`
		<!DOCTYPE html>
		<html>
		<head>
			<title>%s</title>
			<meta charset="utf-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<style>
			/* CSS styles omitted for brevity */
			</style>
		</head>
		<body>
			%s
			<div class="wrapper">
			<main>
				<div class="meta">
				</div>
				<div class="listing">
				<table aria-describedby="summary">
				<thead>
					<tr>
						<th></th>
						<th>Name</th>
						<th class="size">Size</th>
						<th class="timestamp hideable">Modified</th>
						<th class="hideable"></th>
					</tr>
				</thead>
				<tbody>`, folderName, nav))

	if folderName != "/" {
		builder.WriteString("<tr><td></td><td><a href=\"../\"><svg xmlns=\"http://www.w3.org/2000/svg\" class=\"icon icon-tabler icon-tabler-corner-left-up\" width=\"24\" height=\"24\" viewBox=\"0 0 24 24\" stroke-width=\"2\" stroke=\"currentColor\" fill=\"none\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><path stroke=\"none\" d=\"M0 0h24v24H0z\" fill=\"none\"></path><path d=\"M18 18h-6a3 3 0 0 1 -3 -3v-10l-4 4m8 0l-4 -4\"></path></svg><span class=\"go-up\">Up</span></a></td></tr>\n")
	}

	for _, d := range dirs {
		if !*flagShowHidden && strings.HasPrefix(d.Name(), ".") {
			continue
		}
		link := d.Name()
		if d.IsDir() {
			link += "/"
		}
		name := link
		if d.IsDir() {
			builder.WriteString(fmt.Sprintf("<tr class=\"file\"><td></td><td><a href=\"%s\"><svg xmlns=\"http://www.w3.org/2000/svg\" class=\"icon icon-tabler icon-tabler-folder-filled\" width=\"24\" height=\"24\" viewBox=\"0 0 24 24\" stroke-width=\"2\" stroke=\"currentColor\" fill=\"none\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><path stroke=\"none\" d=\"M0 0h24v24H0z\" fill=\"none\"></path><path d=\"M9 3a1 1 0 0 1 .608 .206l.1 .087l2.706 2.707h6.586a3 3 0 0 1 2.995 2.824l.005 .176v8a3 3 0 0 1 -2.824 2.995l-.176 .005h-14a3 3 0 0 1 -2.995 -2.824l-.005 -.176v-11a3 3 0 0 1 2.824 -2.995l.176 -.005h4z\" stroke-width=\"0\" fill=\"#ffb900\"></path></svg><span class=\"name\">%s</span></a></td>", link, name))
			builder.WriteString("<td>—</td>")
		} else {
			builder.WriteString(fmt.Sprintf("<tr class=\"file\"><td></td><td><a href=\"%s\"><svg xmlns=\"http://www.w3.org/2000/svg\" class=\"icon icon-tabler icon-tabler-file\" width=\"24\" height=\"24\" viewBox=\"0 0 24 24\" stroke-width=\"2\" stroke=\"currentColor\" fill=\"none\" stroke-linecap=\"round\" stroke-linejoin=\"round\"><path stroke=\"none\" d=\"M0 0h24v24H0z\" fill=\"none\"></path><path d=\"M14 3v4a1 1 0 0 0 1 1h4\"></path><path d=\"M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z\"></path></svg><span class=\"name\">%s</span></a></td>", link, name))
			builder.WriteString(fmt.Sprintf("<td class=\"size\">%s</td>", formatSize(d.Size())))
		}
		builder.WriteString(fmt.Sprintf("<td class=\"timestamp hideable\">%s</td>", d.ModTime().Format("2006/01/02 15:04:05")))
		builder.WriteString("<td class=\"hideable\"></td></tr>")
	}

	builder.WriteString(`
				</tbody>
				</table>
				</div>
			</main>
			</div>
		</body>
		<footer></footer>
		</html>`)

	return builder.String()
}

func formatSize(bytes int64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
		TB = 1 << 40
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
