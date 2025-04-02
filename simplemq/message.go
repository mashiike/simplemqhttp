package simplemq

import "time"

type Message struct {
	ID                  string `json:"id,omitempty"`
	Content             string `json:"content"`
	CreatedAt           int64  `json:"created_at,omitempty"`
	UpdatedAt           int64  `json:"updated_at,omitempty"`
	ExpiresAt           int64  `json:"expires_at,omitempty"`
	AcquiredAt          int64  `json:"acquired_at,omitempty"`
	VisibilityTimeoutAt int64  `json:"visibility_timeout_at,omitempty"`
}

func (m *Message) CreatedTime() time.Time {
	return time.UnixMilli(m.CreatedAt)
}

func (m *Message) UpdatedTime() time.Time {
	return time.UnixMilli(m.UpdatedAt)
}

func (m *Message) ExpiresTime() time.Time {
	return time.UnixMilli(m.ExpiresAt)
}

func (m *Message) AcquiredTime() time.Time {
	return time.UnixMilli(m.AcquiredAt)
}

func (m *Message) VisibilityTimeoutTime() time.Time {
	return time.UnixMilli(m.VisibilityTimeoutAt)
}
