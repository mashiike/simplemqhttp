package simplemqhttp

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/mashiike/simplemqhttp/simplemq"
	"github.com/mashiike/simplemqhttp/stub"
	"github.com/stretchr/testify/require"
)

func TestListener(t *testing.T) {
	// テスト用のloggerを設定
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// stubサーバーの作成
	apiKey := "test-api-key"
	stubServer := stub.NewServer(apiKey)
	defer stubServer.Close()

	// テスト用のclientを作成
	client := simplemq.NewClient(apiKey, "test-queue")
	client.Endpoint = stubServer.URL()

	// Listenerの作成
	listener := &Listener{
		client: client,
		Logger: logger,
	}
	handledReqesutCh := make(chan []byte, 1)
	// HTTPサーバーのセットアップ
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bs, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r.Body.Close()
			handledReqesutCh <- bs
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success"}`))
		}),
	}

	// ListenerをHTTPサーバーに設定
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("HTTP server error: %v", err)
		}
	}()

	// テストケース
	testCases := []struct {
		name           string
		requestContent string
		expectedStatus int
	}{
		{
			name:           "Simple JSON body",
			requestContent: `{"method":"GET","path":"/"}`,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "JSON body with data",
			requestContent: `{"key":"value","data":"test"}`,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// メッセージをstubサーバーに追加
			msg := stubServer.AddMessage("test-queue", tc.requestContent)
			require.NotNil(t, msg)

			bs := <-handledReqesutCh
			require.Equal(t, tc.requestContent, string(bs))
		})
	}
	err := server.Close()
	require.NoError(t, err)
}
