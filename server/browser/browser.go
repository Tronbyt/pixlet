// Package browser provides the ability to send images to a browser over
// websockets.
package browser

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"
	"tidbyt.dev/pixlet/dist"
	"tidbyt.dev/pixlet/server/fanout"
	"tidbyt.dev/pixlet/server/loader"
)

// Browser provides a structure for serving WebP or GIF images over websockets to
// a web browser.
type Browser struct {
	addr       string             // The address to listen on.
	path       string             // The path to serve the app on.
	title      string             // The title of the HTML document.
	updateChan chan loader.Update // A channel of base64 encoded images.
	watch      bool
	fo         *fanout.Fanout
	r          *http.ServeMux
	loader     *loader.Loader
	serveGif   bool // True if serving GIF, false if serving WebP
}

//go:embed favicon.png
var favicon []byte

// previewData is used to populate the HTML template.
type previewData struct {
	Title     string `json:"title"`
	Image     string `json:"img"`
	ImageType string `json:"img_type"`
	Watch     bool   `json:"-"`
	Err       string `json:"error,omitempty"`
}
type handlerRequest struct {
	ID    string `json:"id"`
	Param string `json:"param"`
}

// NewBrowser sets up a browser structure. Call Run() to kick off the main loops.
func NewBrowser(addr string, servePath string, title string, watch bool, updateChan chan loader.Update, l *loader.Loader, serveGif bool) (*Browser, error) {
	if !strings.HasPrefix(servePath, "/") {
		servePath = "/" + servePath
	}
	if !strings.HasSuffix(servePath, "/") {
		servePath = servePath + "/"
	}

	b := &Browser{
		updateChan: updateChan,
		addr:       addr,
		path:       servePath,
		fo:         fanout.NewFanout(),
		title:      title,
		loader:     l,
		watch:      watch,
		serveGif:   serveGif,
	}

	r := http.NewServeMux()

	// In order for React Router to work, all routes that React Router should
	// manage need to return the root handler.
	r.HandleFunc(servePath, b.rootHandler)
	r.HandleFunc(servePath+"health", b.healthHandler)
	r.HandleFunc(servePath+"oauth-callback", b.rootHandler)

	// This enables the static directory containing JS and CSS to be available
	// at /static.
	r.Handle(fmt.Sprintf("GET %sstatic/", servePath), http.StripPrefix(servePath, http.FileServer(http.FS(dist.Static))))

	r.HandleFunc(servePath+"ws", b.websocketHandler)
	r.HandleFunc(fmt.Sprintf("GET %sfavicon.png", servePath), b.faviconHandler)

	// API endpoints to support the React frontend.
	r.HandleFunc(servePath+"api/v1/preview", b.previewHandler)
	r.HandleFunc(servePath+"api/v1/preview.webp", b.imageHandler)
	r.HandleFunc(servePath+"api/v1/preview.gif", b.imageHandler)
	r.HandleFunc(servePath+"api/v1/push", b.pushHandler)
	r.HandleFunc(fmt.Sprintf("GET %sapi/v1/schema", servePath), b.schemaHandler)
	r.HandleFunc(fmt.Sprintf("POST %sapi/v1/handlers/{handler}", servePath), b.schemaHandlerHandler)
	r.HandleFunc(servePath+"api/v1/ws", b.websocketHandler)
	b.r = r

	return b, nil
}

// Run starts the server process and runs forever in a blocking fashion. The
// main routines include an update watcher to process incomming changes to the
// image and running the http handlers.
func (b *Browser) Run() error {
	defer b.fo.Quit()

	g := errgroup.Group{}
	g.Go(b.updateWatcher)
	g.Go(b.serveHTTP)

	return g.Wait()
}

func (b *Browser) faviconHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Write(favicon)
}

func (b *Browser) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
}

func (b *Browser) schemaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(b.loader.GetSchema())
}

func (b *Browser) schemaHandlerHandler(w http.ResponseWriter, r *http.Request) {
	handler := r.PathValue("handler")
	if handler == "" {
		w.WriteHeader(404)
		fmt.Fprintln(w, "no handler")
		return
	}

	msg := &handlerRequest{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(msg)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, err)
		return
	}

	data, err := b.loader.CallSchemaHandler(r.Context(), handler, msg.Param)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(data))
}

func (b *Browser) imageHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(500)
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	config := make(map[string]string)
	for k, val := range r.Form {
		config[k] = val[0]
	}

	img, err := b.loader.LoadApplet(config)
	if err != nil {
		http.Error(w, "loading applet", http.StatusInternalServerError)
		return
	}

	img_type := "image/webp"
	if b.serveGif {
		img_type = "image/gif"
	}
	w.Header().Set("Content-Type", img_type)

	data, err := base64.StdEncoding.DecodeString(img)
	if err != nil {
		http.Error(w, "decoding image", http.StatusInternalServerError)
		return
	}

	w.Write(data)
}

func (b *Browser) previewHandler(w http.ResponseWriter, r *http.Request) {
	// Parse the request form so we can use it as config values.
	if err := r.ParseMultipartForm(100); err != nil {
		log.Printf("form parsing failed: %+v", err)
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}
	config := make(map[string]string)
	for k, val := range r.Form {
		config[k] = val[0]
	}

	img, err := b.loader.LoadApplet(config)
	img_type := "webp"
	if b.serveGif {
		img_type = "gif"
	}
	data := &previewData{
		Image:     img,
		ImageType: img_type,
		Title:     b.title,
	}
	if err != nil {
		data.Err = err.Error()
	}

	d, err := json.Marshal(data)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(d)
}

func (b *Browser) websocketHandler(w http.ResponseWriter, r *http.Request) {
	if !b.watch {
		return
	}

	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error establishing a new connection %v\n", err)
		return
	}

	b.fo.NewClient(conn)
}

func (b *Browser) updateWatcher() error {
	img_type := "webp"
	if b.serveGif {
		img_type = "gif"
	}

	for {
		select {
		case up := <-b.updateChan:
			b.fo.Broadcast(
				fanout.WebsocketEvent{
					Type:      fanout.EventTypeImage,
					Message:   up.Image,
					ImageType: img_type,
				},
			)

			if up.Err != nil {
				b.fo.Broadcast(
					fanout.WebsocketEvent{
						Type:    fanout.EventTypeErr,
						Message: up.Err.Error(),
					},
				)
			}

			if up.Schema != "" {
				b.fo.Broadcast(
					fanout.WebsocketEvent{
						Type:    fanout.EventTypeSchema,
						Message: up.Schema,
					},
				)
			}
		}
	}
}
func (b *Browser) rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	t, err := template.New("index").Parse(string(dist.Index))
	if err != nil {
		http.Error(w, fmt.Sprintf("error loading index template: %v", err), http.StatusInternalServerError)
		return
	}
	config := map[string]any{}
	if b.path != "/" {
		config["Base"] = b.path
	}
	if err := t.Execute(w, config); err != nil {
		http.Error(w, fmt.Sprintf("error executing template: %v", err), http.StatusInternalServerError)
	}
}
