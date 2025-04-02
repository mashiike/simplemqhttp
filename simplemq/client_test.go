package simplemq_test

import (
	"context"
	"testing"
	"time"

	"github.com/mashiike/simplemqhttp/simplemq"
	"github.com/mashiike/simplemqhttp/stub"
	"github.com/stretchr/testify/require"
)

func TestClient(t *testing.T) {
	const (
		testAPIKey = "test-api-key"
		testQueue  = "test-queue"
	)

	// スタブサーバーの作成
	server := stub.NewServer(testAPIKey)
	defer server.Close()

	// スタブサーバーを使用するクライアントを作成
	client := simplemq.NewClient(testAPIKey, testQueue)
	client.Endpoint = server.URL()

	// テスト用のコンテキスト
	ctx := context.Background()

	t.Run("SendMessage", func(t *testing.T) {
		// テスト用のメッセージ
		content := "hello, world"

		// メッセージを送信
		msg, err := client.SendMessage(ctx, content)
		require.NoError(t, err)
		require.NotEmpty(t, msg.ID)
		require.Equal(t, content, msg.Content)
		require.Greater(t, msg.CreatedAt, int64(0))
		require.Equal(t, msg.CreatedAt, msg.UpdatedAt)

		// スタブサーバーの状態を確認
		require.Equal(t, 1, server.GetQueueSize(testQueue))
	})

	t.Run("ReceiveMessages", func(t *testing.T) {
		// テスト前にキューを空にする
		server.Reset()

		// メッセージがない場合
		msgs, err := client.ReceiveMessages(ctx)
		require.NoError(t, err)
		require.Empty(t, msgs)

		// メッセージをキューに追加
		content1 := "test message 1"
		content2 := "test message 2"
		server.AddMessage(testQueue, content1)
		server.AddMessage(testQueue, content2)

		// メッセージを受信
		msgs, err = client.ReceiveMessages(ctx)
		require.NoError(t, err)
		require.Len(t, msgs, 2)

		// メッセージ内容の確認
		contents := []string{msgs[0].Content, msgs[1].Content}
		require.Contains(t, contents, content1)
		require.Contains(t, contents, content2)

		// visibilityTimeout の設定確認
		now := time.Now().UnixMilli()
		for _, msg := range msgs {
			require.Greater(t, msg.VisibilityTimeoutAt, now)
		}
	})

	t.Run("DeleteMessage", func(t *testing.T) {
		// テスト前にキューを空にする
		server.Reset()

		// メッセージを追加
		msg := server.AddMessage(testQueue, "message to delete")
		require.Equal(t, 1, server.GetQueueSize(testQueue))

		// メッセージを削除
		err := client.DeleteMessage(ctx, msg.ID)
		require.NoError(t, err)
		require.Equal(t, 0, server.GetQueueSize(testQueue))

		// 存在しないメッセージを削除 (エラーになることを確認)
		err = client.DeleteMessage(ctx, "non-existent-id")
		require.Error(t, err)
		var apiErr *simplemq.APIError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, 404, apiErr.Code)
	})

	t.Run("ExtendVisibilityTimeout", func(t *testing.T) {
		// テスト前にキューを空にする
		server.Reset()

		// メッセージを追加
		msg := server.AddMessage(testQueue, "message to extend")

		// visibilityTimeout を初期化
		msg.VisibilityTimeoutAt = 0

		// visibilityTimeout を延長
		updatedMsg, err := client.ExtendVisibilityTimeout(ctx, msg.ID)
		require.NoError(t, err)
		require.Equal(t, msg.ID, updatedMsg.ID)
		require.Greater(t, updatedMsg.VisibilityTimeoutAt, int64(0))

		// 存在しないメッセージの処理
		_, err = client.ExtendVisibilityTimeout(ctx, "non-existent-id")
		require.Error(t, err)
		var apiErr *simplemq.APIError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, 404, apiErr.Code)
	})

	t.Run("AuthenticationFailed", func(t *testing.T) {
		// 間違ったAPIキーを持つクライアント
		invalidClient := simplemq.NewClient("wrong-api-key", testQueue)
		invalidClient.Endpoint = server.URL()

		// 認証エラーになることを確認
		_, err := invalidClient.SendMessage(ctx, "test message")
		require.Error(t, err)
		var apiErr *simplemq.APIError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, 401, apiErr.Code)
	})
}
