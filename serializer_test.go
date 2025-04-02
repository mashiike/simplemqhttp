package simplemqhttp

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyOnlySerializer(t *testing.T) {
	serializer := &BodyOnlySerializer{}

	t.Run("Deserialize basic JSON content", func(t *testing.T) {
		content := `{"id":123,"name":"test"}`
		req, err := serializer.Deserialize(content)

		require.NoError(t, err)
		assert.Equal(t, "POST", req.Method)
		assert.Equal(t, "/", req.URL.Path)

		// ボディの確認
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, content, string(body))
	})

	t.Run("Serialize simple request", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/", strings.NewReader(`{"data":"value"}`))
		require.NoError(t, err)

		serialized, err := serializer.Serialize(req)
		require.NoError(t, err)
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(`{"data":"value"}`)), serialized)
	})

	t.Run("Serialize empty request body", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/users", nil)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		serialized, err := serializer.Serialize(req)
		require.NoError(t, err)

		// 空のレスポンスを確認
		assert.Equal(t, "", serialized)
	})

	t.Run("Serialize and deserialize roundtrip", func(t *testing.T) {
		// リクエスト作成
		req, err := http.NewRequest("POST", "/api/items",
			strings.NewReader(`{"name":"test item","price":100}`))
		require.NoError(t, err)

		// シリアライズ
		serialized, err := serializer.Serialize(req)
		require.NoError(t, err)

		// デシリアライズ
		deserializedReq, err := serializer.Deserialize(serialized)
		require.NoError(t, err)

		// ボディ内容の確認
		body, err := io.ReadAll(deserializedReq.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"name":"test item","price":100}`, string(body))
	})
}
