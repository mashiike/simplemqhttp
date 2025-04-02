package simplemqhttp

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mashiike/simplemqhttp/simplemq"
)

type Transport struct {
	client *simplemq.Client
	// Serializer は、リクエストをシリアライズするためのインターフェースです。
	// 未指定の場合は、BodyOnlySerializer が使用されます。
	Serializer Serializer
}

func NewTransport(apikey string, queue string) *Transport {
	client := simplemq.NewClient(apikey, queue)
	return NewTransportWithClient(client)
}

func NewTransportWithClient(client *simplemq.Client) *Transport {
	return &Transport{
		client: client,
	}
}

var _ http.RoundTripper = &Transport{}

func (t *Transport) serializer() Serializer {
	if t.Serializer != nil {
		return t.Serializer
	}
	return &BodyOnlySerializer{}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	serializer := t.serializer()
	content, err := serializer.Serialize(req)
	if err != nil {
		return nil, err
	}
	msg, err := t.client.SendMessage(req.Context(), content)
	var builder strings.Builder
	if err != nil {
		var apiErr *simplemq.APIError
		if !errors.As(err, &apiErr) {
			return nil, err
		}
		builder.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", apiErr.Code, http.StatusText(apiErr.Code)))
		headers := http.Header{
			"Content-Type":        []string{"text/plain"},
			"Content-Length":      []string{strconv.Itoa(len(apiErr.Message))},
			"SimpleMQ-Queue-Name": []string{t.client.Queue},
		}
		headers.Write(&builder)
		builder.WriteString("\r\n")
		builder.WriteString(apiErr.Message)
	} else {
		builder.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", http.StatusAccepted, http.StatusText(http.StatusAccepted)))
		headers := http.Header{
			"Content-Type":             []string{"text/plain"},
			"Content-Length":           []string{"0"},
			"SimpleMQ-Queue-Name":      []string{t.client.Queue},
			"SimpleMQ-Message-ID":      []string{msg.ID},
			"SimpleMQ-Message-Created": []string{msg.CreatedTime().Format(time.RFC3339)},
		}
		headers.Write(&builder)
		builder.WriteString("\r\n")
	}
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(builder.String())), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
