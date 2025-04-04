package simplemqhttp

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mashiike/simplemqhttp/simplemq"
)

// ResponseHandler は、HTTP レスポンスを処理するためのインターフェースです。
type ResponseHandler interface {
	HandleResponse(resp *http.Response, req *http.Request) error
}

// Listener は、SimpleMQ からメッセージを受信して HTTP リクエストに変換するための net.Listener 実装です。
type Listener struct {
	client           *simplemq.Client
	mu               sync.Mutex
	acceptedMessages []simplemq.Message
	BaseContext      func() context.Context
	Serializer       Serializer
	Logger           *slog.Logger
	ResponseHandler  ResponseHandler
	baseCtx          context.Context
	baseCancel       context.CancelFunc
}

// NewListener は、新しい Listener を作成します。
func NewListener(apikey string, queue string) *Listener {
	client := simplemq.NewClient(apikey, queue)
	return NewListenerWithClient(client)
}

// NewListenerWithClient は、既存の SimpleMQ クライアントを使用して新しい Listener を作成します。
func NewListenerWithClient(client *simplemq.Client) *Listener {
	return &Listener{
		client: client,
	}
}

var _ net.Listener = &Listener{}

func (l *Listener) baseContext() context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.baseCtx != nil {
		return l.baseCtx
	}
	if l.BaseContext != nil {
		l.baseCtx, l.baseCancel = context.WithCancel(l.BaseContext())
	} else {
		l.baseCtx, l.baseCancel = context.WithCancel(context.Background())
	}
	return l.baseCtx
}

func (l *Listener) serializer() Serializer {
	if l.Serializer != nil {
		return l.Serializer
	}
	return &BodyOnlySerializer{}
}

func (l *Listener) accept(ctx context.Context) (*simplemq.Message, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for len(l.acceptedMessages) == 0 {
		time.Sleep(200 * time.Millisecond)
		msg, err := l.client.ReceiveMessages(ctx)
		if err != nil {
			return nil, err
		}
		l.acceptedMessages = append(l.acceptedMessages, msg...)
	}

	msg := l.acceptedMessages[0]
	l.acceptedMessages = l.acceptedMessages[1:]
	return &msg, nil
}

func (l *Listener) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

// Accept は、次の接続を待機して返します。
func (l *Listener) Accept() (net.Conn, error) {
	ctx := l.baseContext()
	for {
		msg, err := l.accept(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				l.logger().Debug("accept canceled")
				return nil, net.ErrClosed
			}
			return nil, err
		}
		if time.Until(msg.VisibilityTimeoutTime()) <= 0 {
			l.logger().Debug("accepted message is expired", "msg", msg)
			continue
		}
		l.logger().Debug("accepted message", "msg", msg)
		conn := newConn(l.Addr(), *msg, l.serializer(), l.client, l.logger())
		if l.ResponseHandler != nil {
			conn.respHandler = l.ResponseHandler
		}
		return conn, nil
	}
}

// Close はリスナーを閉じます。
// ブロックされた Accept 操作はすべてブロック解除され、エラーを返します。
func (l *Listener) Close() error {
	if l.baseCancel != nil {
		l.baseCancel()
		l.baseCancel = nil
	}
	return nil
}

// Addr はリスナーのネットワークアドレスを返します。
func (l *Listener) Addr() net.Addr {
	return Addr(l.client.Queue)
}
