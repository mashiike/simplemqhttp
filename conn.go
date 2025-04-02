package simplemqhttp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mashiike/simplemqhttp/simplemq"
)

// Conn は、SimpleMQ から受信したメッセージを HTTP リクエストに変換するための net.Conn 実装です。
type Conn struct {
	addr         net.Addr
	msg          simplemq.Message
	serializer   Serializer
	client       *simplemq.Client
	extendCtx    context.Context
	extendCancel context.CancelFunc
	extendWg     sync.WaitGroup
	extendErr    error
	reqBytes     []byte
	initErr      error
	logger       *slog.Logger
	req          *http.Request
	respBuffer   bytes.Buffer
	respHandler  ResponseHandler
}

var _ net.Conn = &Conn{}

func newConn(addr net.Addr, msg simplemq.Message, serializer Serializer, client *simplemq.Client, logger *slog.Logger) *Conn {
	c := &Conn{
		addr:       addr,
		msg:        msg,
		serializer: serializer,
		client:     client,
		logger:     logger,
	}
	c.init()
	return c
}

func (c *Conn) init() {
	c.extendCtx, c.extendCancel = context.WithCancel(context.Background())
	req, err := c.serializer.Deserialize(c.msg.Content)
	if err != nil {
		c.initErr = err
		return
	}
	req.Header.Add("SimpleMQ-Message-ID", c.msg.ID)
	req.Header.Add("SimpleMQ-Message-Created", c.msg.CreatedTime().Format(time.RFC3339))
	req.Header.Add("SimpleMQ-Message-Visibility-Timeout", c.msg.VisibilityTimeoutTime().Format(time.RFC3339))
	req.Header.Add("SimpleMQ-Queue-Name", c.client.Queue)
	c.extendWg.Add(1)
	go func() {
		defer func() {
			c.logger.Debug("end extend visibility timeout", "message_id", c.msg.ID)
			c.extendWg.Done()
		}()
		c.logger.Debug("start extend visibility timeout", "message_id", c.msg.ID)
		timer := time.NewTimer(time.Duration(float64(time.Until(c.msg.VisibilityTimeoutTime())) * 0.9))
		for {
			select {
			case <-c.extendCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// extend visibility timeout
			extendedMsg, err := c.client.ExtendVisibilityTimeout(c.extendCtx, c.msg.ID)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					c.extendErr = err
				}
				return
			}
			c.logger.Debug("extend visibility timeout", "message_id", c.msg.ID, "visibility_timeout_at", extendedMsg.VisibilityTimeoutTime().Format(time.RFC3339))
			c.msg.VisibilityTimeoutAt = extendedMsg.VisibilityTimeoutAt
			timer.Reset(time.Duration(float64(time.Until(c.msg.VisibilityTimeoutTime())) * 0.9))
		}
	}()
	c.req = req
	var buf bytes.Buffer
	if err := req.Write(&buf); err != nil {
		c.initErr = err
		return
	}
	c.reqBytes = buf.Bytes()
}

// Read implements the net.Conn Read method.
func (c *Conn) Read(b []byte) (n int, err error) {
	if c.initErr != nil {
		return 0, fmt.Errorf("failed to initialize connection: %w", c.initErr)
	}
	if c.extendErr != nil {
		return 0, fmt.Errorf("failed to extend visibility timeout: %w", c.extendErr)
	}
	if len(c.reqBytes) == 0 {
		return 0, net.ErrClosed
	}
	n = copy(b, c.reqBytes)
	c.reqBytes = c.reqBytes[n:]
	return n, nil
}

// Write implements the net.Conn Write method.
func (c *Conn) Write(b []byte) (n int, err error) {
	if c.extendErr != nil {
		return 0, fmt.Errorf("failed to extend visibility timeout: %w", c.extendErr)
	}
	if len(b) == 0 {
		return 0, nil
	}
	return c.respBuffer.Write(b)
}

// Close implements the net.Conn Close method.
func (c *Conn) Close() error {
	if c.extendCancel != nil {
		c.extendCancel()
		c.extendWg.Wait()
	}

	// レスポンスが空の場合は何もしない
	if c.respBuffer.Len() == 0 {
		return nil
	}
	resp, err := http.ReadResponse(bufio.NewReader(&c.respBuffer), c.req)
	if err != nil {
		c.logger.Error("failed to serialize response", "err", err, "message_id", c.msg.ID)
		return fmt.Errorf("failed to serialize response: %w", err)
	}

	// ステータスコードをチェック
	statusCode := resp.StatusCode
	c.logger.Debug("response status", "status_code", statusCode, "message_id", c.msg.ID)

	if c.respHandler != nil {
		if err := c.respHandler.HandleResponse(resp, c.req); err != nil {
			c.logger.Error("failed to handle response", "err", err, "message_id", c.msg.ID)
			return fmt.Errorf("failed to handle response: %w", err)
		}
	}
	// 2xx系のレスポンスならメッセージを削除
	if statusCode >= 200 && statusCode < 300 {
		c.logger.Debug("deleting message due to successful response", "message_id", c.msg.ID)
		if err := c.client.DeleteMessage(context.Background(), c.msg.ID); err != nil {
			c.logger.Error("failed to delete message", "err", err, "message_id", c.msg.ID)
			return fmt.Errorf("failed to delete message: %w", err)
		}
		return nil
	}
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		c.logger.Debug("message not deleted due to Retry-After header", "message_id", c.msg.ID)
		seconds, err := strconv.Atoi(retryAfter)
		if err != nil {
			c.logger.Warn("unexpected Retry-After header, must be a number of seconds", "message_id", c.msg.ID, "header", retryAfter)
			return nil
		}
		for time.Until(c.msg.VisibilityTimeoutTime()) < time.Duration(seconds)*time.Second {
			extendedMsg, err := c.client.ExtendVisibilityTimeout(context.Background(), c.msg.ID)
			if err != nil {
				c.logger.Warn("failed to extend visibility timeout for Retry-After", "err", err, "message_id", c.msg.ID, "header", retryAfter)
				return nil
			}
			c.msg.VisibilityTimeoutAt = extendedMsg.VisibilityTimeoutAt
			c.logger.Debug("extended visibility timeout for Retry-After", "message_id", c.msg.ID, "visibility_timeout_at", extendedMsg.VisibilityTimeoutTime().Format(time.RFC3339))
		}
	}
	return nil
}

// LocalAddr implements the net.Conn LocalAddr method.
func (c *Conn) LocalAddr() net.Addr {
	return c.addr
}

// RemoteAddr implements the net.Conn RemoteAddr method.
func (c *Conn) RemoteAddr() net.Addr {
	return c.addr
}

// SetDeadline implements the net.Conn SetDeadline method.
func (c *Conn) SetDeadline(t time.Time) error {
	if c.extendCancel != nil {
		c.extendCancel()
		c.extendWg.Wait()
	}

	if t.IsZero() {
		return nil
	}

	// Extend visibility timeout to the deadline time
	deadline := time.Until(t)
	if deadline <= 0 {
		return nil // 既に期限切れの場合は何もしない
	}

	c.logger.Debug("extending visibility timeout to reach deadline",
		"message_id", c.msg.ID,
		"deadline", t.Format(time.RFC3339))

	// 現在のタイムアウト時刻
	currentTimeout := c.msg.VisibilityTimeoutTime()

	// 目標のタイムアウト時刻に達するまで延長を繰り返す
	maxAttempts := 10
	sleepDuration := 200 * time.Millisecond
	for attempts := 0; currentTimeout.Before(t) && attempts < maxAttempts; attempts++ {
		extendedMsg, err := c.client.ExtendVisibilityTimeout(context.Background(), c.msg.ID)
		if err != nil {
			return fmt.Errorf("failed to extend visibility timeout to deadline: %w", err)
		}

		// タイムアウト時刻を更新
		c.msg.VisibilityTimeoutAt = extendedMsg.VisibilityTimeoutAt
		currentTimeout = c.msg.VisibilityTimeoutTime()

		c.logger.Debug("extended visibility timeout step",
			"message_id", c.msg.ID,
			"current_timeout", currentTimeout.Format(time.RFC3339),
			"target_deadline", t.Format(time.RFC3339))

		// 少し待機して、APIの呼び出し頻度を制限
		time.Sleep(sleepDuration)
	}

	c.logger.Debug("successfully extended visibility timeout to reach deadline",
		"message_id", c.msg.ID,
		"deadline", t.Format(time.RFC3339),
		"visibility_timeout_at", currentTimeout.Format(time.RFC3339))

	return nil
}

// SetReadDeadline implements the net.Conn SetReadDeadline method.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.SetDeadline(t)
}

// SetWriteDeadline implements the net.Conn SetWriteDeadline method.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.SetDeadline(t)
}
