package handler

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() any {
		// 使用默认压缩级别在 CPU 与压缩率间取得均衡
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gw          *gzip.Writer
	wroteHeader bool
	compressed  bool
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	if status == http.StatusNoContent || status == http.StatusNotModified || g.Header().Get("Content-Encoding") != "" {
		g.ResponseWriter.WriteHeader(status)
		return
	}
	g.compressed = true
	g.Header().Del("Content-Length")
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Add("Vary", "Accept-Encoding")
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if !g.compressed {
		return g.ResponseWriter.Write(b)
	}
	if g.gw == nil {
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(g.ResponseWriter)
		g.gw = gw
	}
	return g.gw.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.gw != nil {
		_ = g.gw.Flush()
	}
	if fl, ok := g.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (g *gzipResponseWriter) finish() {
	if g.gw != nil {
		_ = g.gw.Close()
		g.gw.Reset(io.Discard)
		gzipWriterPool.Put(g.gw)
		g.gw = nil
	}
}

// withGzip 在客户端声明 Accept-Encoding: gzip 时对响应做流式 gzip 压缩。
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		grw := &gzipResponseWriter{ResponseWriter: w}
		defer grw.finish()
		next.ServeHTTP(grw, r)
	})
}
