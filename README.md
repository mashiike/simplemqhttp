# simplemqhttp

**⚠️ このライブラリはPoC（概念実証）として開発されています ⚠️**

さくらクラウドのSimpleMQをGolangのnet/httpインターフェースで利用するためのインテグレーションライブラリです。

## 概要

simplemqhttpは、[さくらクラウドが提供するメッセージキューサービス「SimpleMQ」](https://manual.sakura.ad.jp/cloud/appliance/simplemq/index.html)をGolangの標準ライブラリであるnet/httpの枠組みの中で簡単に利用できるようにするライブラリです。このライブラリを使用することで、HTTPリクエスト/レスポンスの形式でSimpleMQとやり取りすることができます。

## インストール

```
go get github.com/mashiike/simplemqhttp
```

## 使用例

リポジトリの `_examples` ディレクトリに具体的な実装例があります。サーバー側とクライアント側の両方の例が含まれています。
このサンプルは以下のように動作します。

クライアント側:

```shell
$ go run _examples/client/main.go --queue test01 --content "this is a pen\!"
HTTP/1.1 202 Accepted
Content-Length: 0
Content-Type: text/plain
Simplemq-Message-Created: 2025-04-03T01:47:56+09:00
Simplemq-Message-Id: 0195f766-fbbb-7b80-8a0c-09962cc891e7
Simplemq-Queue-Name: test01
```

サーバー側:
```shell
$ go run _examples/server/main.go --queue test01                    
2025/04/03 01:48:25 server started queue test01
POST / HTTP/1.1
Content-Length: 14
Simplemq-Message-Created: 2025-04-03T01:47:56+09:00
Simplemq-Message-Id: 0195f766-fbbb-7b80-8a0c-09962cc891e7
Simplemq-Message-Visibility-Timeout: 2025-04-03T01:48:55+09:00
Simplemq-Queue-Name: test01
User-Agent: Go-http-client/1.1

this is a pen!
```

### サーバー側の実装例

SimpleMQからメッセージを受信し、HTTPリクエストとして処理するサーバーの例:

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"

    "github.com/mashiike/simplemqhttp"
)

func main() {
    // SimpleMQへのアクセスに必要なAPIキーとキュー名
    apikey := os.Getenv("SACLOUD_API_KEY")
    queueName := "your-queue-name"
    
    // Listenerの作成
    listener := simplemqhttp.NewListener(apikey, queueName)
    
    // HTTPサーバーの設定
    server := &http.Server{
        Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ここでリクエストを処理
            // リクエストヘッダーにはSimpleMQのメッセージ情報が含まれています
            msgID := r.Header.Get("SimpleMQ-Message-ID")
            log.Printf("Received message: %s", msgID)
            
            // 成功レスポンスを返すとメッセージは自動的に削除されます
            w.WriteHeader(http.StatusOK)
        }),
    }
    
    // シグナル処理
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()
    
    // サーバー起動
    go func() {
        if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
            log.Fatalf("Server error: %v", err)
        }
    }()
    
    log.Printf("Server started with queue: %s", queueName)
    <-ctx.Done()
    
    // シャットダウン
    log.Println("Shutting down...")
    if err := server.Shutdown(context.Background()); err != nil {
        log.Fatalf("Server shutdown error: %v", err)
    }
}
```

サーバー側は、SimpleMQからメッセージを受信してHTTPのプロトコルに変換するnet.Listenerを実装しています。これにより、HTTPリクエストとしてメッセージを受信し、処理することができます。

### クライアント側の実装例

HTTPリクエストをSimpleMQメッセージとして送信する例:

```go
package main

import (
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/mashiike/simplemqhttp"
)

func main() {
    // SimpleMQへのアクセスに必要なAPIキーとキュー名
    apikey := os.Getenv("SACLOUD_API_KEY")
    queueName := "your-queue-name"
    
    // Transportの作成
    transport := simplemqhttp.NewTransport(apikey, queueName)
    
    // HTTPクライアントの作成
    client := &http.Client{
        Transport: transport,
    }
    
    // リクエスト送信
    // これはSimpleMQにメッセージとして送信されます
    resp, err := client.Post("/api/endpoint", "application/json", strings.NewReader(`{"data":"value"}`))
    if err != nil {
        log.Fatalf("Request error: %v", err)
    }
    defer resp.Body.Close()
    
    // レスポンスのステータスコードはAccepted (202)になります
    log.Printf("Response status: %s", resp.Status)
    
    // SimpleMQメッセージIDを取得
    msgID := resp.Header.Get("SimpleMQ-Message-ID")
    log.Printf("Message ID: %s", msgID)
}
```

クライアント側は、HTTPリクエストをSimpleMQメッセージとして送信するためのTransportを実装しています。これにより、HTTPリクエストをSimpleMQに送信し、レスポンスを受け取ることができます。

## カスタマイズ

### カスタムシリアライザ

デフォルトでは、リクエストのボディのみがメッセージとして送信されますが、独自のシリアライザを実装することで、メソッド、パス、ヘッダーなどを含めた完全なHTTPリクエストをシリアライズすることができます。

```go
// Serializerインターフェースを実装したカスタムシリアライザを作成
type CustomSerializer struct {}

func (s *CustomSerializer) Serialize(req *http.Request) (string, error) {
    // カスタムロジックでリクエストをシリアライズ
}

func (s *CustomSerializer) Deserialize(content string) (*http.Request, error) {
    // カスタムロジックでメッセージをリクエストにデシリアライズ
}

// カスタムシリアライザを使用
listener := simplemqhttp.NewListener(apikey, queueName)
listener.Serializer = &CustomSerializer{}

transport := simplemqhttp.NewTransport(apikey, queueName)
transport.Serializer = &CustomSerializer{}
```

### レスポンスハンドラ

サーバー側では、レスポンスハンドラを実装することで、HTTPレスポンスに基づいたカスタム処理を行うことができます。

```go
type CustomResponseHandler struct {}

func (h *CustomResponseHandler) HandleResponse(resp *http.Response, req *http.Request) error {
    // レスポンスに基づいたカスタム処理
    return nil
}

// カスタムレスポンスハンドラを使用
listener := simplemqhttp.NewListener(apikey, queueName)
listener.ResponseHandler = &CustomResponseHandler{}
```

## ライセンス

MIT License
