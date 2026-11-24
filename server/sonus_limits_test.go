package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LimitRequestBody", func() {
	const limit = 64
	// the handler decodes JSON as the native API's do, and says what it got
	handler := LimitRequestBody(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v any
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	send := func(body, contentType string, declared bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/queue", strings.NewReader(body))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if !declared {
			r.ContentLength = -1 // sent chunked: the size is only known while reading
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	big := `{"ids":["` + strings.Repeat("a", 2*limit) + `"]}`

	It("lets a body within the limit through", func() {
		w := send(`{"ids":["1","2"]}`, "application/json", true)
		Expect(w.Code).To(Equal(http.StatusOK))
	})

	It("answers 413 to a body declared larger than the limit, without calling the handler", func() {
		w := send(big, "application/json", true)
		Expect(w.Code).To(Equal(http.StatusRequestEntityTooLarge))
	})

	It("stops reading an undeclared body at the limit", func() {
		w := send(big, "", false)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(w.Body.String()).To(ContainSubstring("too large"))
	})

	DescribeTable("leaves uploads to their own limits",
		func(contentType string) {
			var read int
			h := LimitRequestBody(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				read = len(b)
			}))
			r := httptest.NewRequest(http.MethodPost, "/api/playlist/1/image", strings.NewReader(big))
			r.Header.Set("Content-Type", contentType)
			h.ServeHTTP(httptest.NewRecorder(), r)
			Expect(read).To(Equal(len(big)))
		},
		Entry("multipart", "multipart/form-data; boundary=x"),
		Entry("an image body (Jellyfin)", "image/jpeg"),
	)
})
