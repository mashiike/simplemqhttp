package simplemqhttp

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mashiike/simplemqhttp/simplemq"
	"github.com/mashiike/simplemqhttp/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransport(t *testing.T) {
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

	// Transportの作成
	transport := NewTransportWithClient(client)

	// テストケース
	testCases := []struct {
		name           string
		method         string
		path           string
		body           string
		headers        map[string]string
		expectedStatus int
	}{
		{
			name:           "Simple GET request",
			method:         "GET",
			path:           "/test",
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "POST request with body",
			method:         "POST",
			path:           "/data",
			body:           `{"key":"value","data":"test"}`,
			headers:        map[string]string{"Content-Type": "application/json"},
			expectedStatus: http.StatusAccepted,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// HTTPリクエストの作成
			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req, err := http.NewRequest(tc.method, tc.path, bodyReader)
			require.NoError(t, err)

			// ヘッダーの設定
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			// トランスポートを使用してリクエストを実行
			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)

			// レスポンスのステータスコードを検証
			assert.Equal(t, tc.expectedStatus, resp.StatusCode)

			// メッセージIDヘッダーが存在することを確認
			msgID := resp.Header.Get("SimpleMQ-Message-ID")
			assert.NotEmpty(t, msgID, "Message ID should be present in response headers")

			// キュー名が正しいことを確認
			queueName := resp.Header.Get("SimpleMQ-Queue-Name")
			assert.Equal(t, "test-queue", queueName)

			// メッセージがキューに存在することを確認
			msg := stubServer.GetMessage("test-queue", msgID)
			assert.NotNil(t, msg, "Message should exist in the queue")

			// リクエストボディがメッセージに正しく保存されていることを確認
			if tc.body != "" {
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(tc.body)), msg.Content)
			}

			logger.Debug("Test completed", "message_id", msgID, "queue", queueName)
		})
	}
}

func TestTransportAPIError(t *testing.T) {
	// stubサーバーの作成
	apiKey := "test-api-key"
	stubServer := stub.NewServer(apiKey)
	defer stubServer.Close()

	// 不正なAPI Keyでclientを作成（認証エラーを発生させる）
	client := simplemq.NewClient("invalid-api-key", "test-queue")
	client.Endpoint = stubServer.URL()

	// Transportの作成
	transport := NewTransportWithClient(client)

	// リクエストの作成
	req, err := http.NewRequest("GET", "/test", nil)
	require.NoError(t, err)

	// トランスポートを使用してリクエストを実行
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	// 認証エラーで401が返されることを検証
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// レスポンスボディを確認
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "unauthorized")
}

func TestTransportWithContext(t *testing.T) {
	// stubサーバーの作成
	apiKey := "test-api-key"
	stubServer := stub.NewServer(apiKey)
	defer stubServer.Close()

	// テスト用のclientを作成
	client := simplemq.NewClient(apiKey, "test-queue")
	client.Endpoint = stubServer.URL()

	// Transportの作成
	transport := NewTransportWithClient(client)

	// コンテキストを持つリクエストの作成
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "POST", "/test", strings.NewReader(`{"test":"data"}`))
	require.NoError(t, err)

	// リクエスト実行前にコンテキストをキャンセル
	cancel()

	// トランスポートを使用してリクエストを実行（コンテキストがキャンセルされているのでエラーになるはず）
	resp, err := transport.RoundTrip(req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "context canceled")
}

type CustomSerializer struct {
	mu      sync.Mutex
	storoed []*http.Request
}

func (s *CustomSerializer) Serialize(req *http.Request) (string, error) {
	if req == nil {
		return "", errors.New("request is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	indexNumber := len(s.storoed)
	s.storoed = append(s.storoed, req.Clone(context.Background()))
	return strconv.Itoa(indexNumber), nil
}

func (s *CustomSerializer) Deserialize(content string) (*http.Request, error) {
	if content == "" {
		return nil, errors.New("content is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	indexNumber, err := strconv.Atoi(content)
	if err != nil || indexNumber < 0 || indexNumber >= len(s.storoed) {
		return nil, errors.New("invalid index number")
	}
	return s.storoed[indexNumber].Clone(context.Background()), nil
}

func TestTransportCustomSerializer(t *testing.T) {
	// stubサーバーの作成
	apiKey := "test-api-key"
	stubServer := stub.NewServer(apiKey)
	defer stubServer.Close()

	// テスト用のclientを作成
	client := simplemq.NewClient(apiKey, "test-queue")
	client.Endpoint = stubServer.URL()

	// カスタムシリアライザを持つTransportの作成
	transport := NewTransportWithClient(client)
	transport.Serializer = &CustomSerializer{}

	// リクエストの作成
	req, err := http.NewRequest("POST", "/custom", strings.NewReader(`{"custom":"serializer"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// トランスポートを使用してリクエストを実行
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	// レスポンスを検証
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	// メッセージIDを取得
	msgID := resp.Header.Get("SimpleMQ-Message-ID")
	assert.NotEmpty(t, msgID)

	// メッセージの内容を確認
	msg := stubServer.GetMessage("test-queue", msgID)
	assert.NotNil(t, msg)
	assert.Equal(t, `0`, msg.Content)
}

func TestTransportHTTPClient(t *testing.T) {
	// stubサーバーの作成
	apiKey := "test-api-key"
	stubServer := stub.NewServer(apiKey)
	defer stubServer.Close()

	// テスト用のclientを作成
	client := simplemq.NewClient(apiKey, "test-queue")
	client.Endpoint = stubServer.URL()

	// Transportを使ったHTTPクライアントの作成
	transport := NewTransportWithClient(client)
	httpClient := &http.Client{
		Transport: transport,
	}

	// リクエストの実行
	resp, err := httpClient.Post("/api/data", "application/json", strings.NewReader(`{"client":"test"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	// レスポンスを検証
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	// リクエストがキューに送信されたことを確認
	queueSize := stubServer.GetQueueSize("test-queue")
	assert.Equal(t, 1, queueSize, "One message should be in the queue")
}
