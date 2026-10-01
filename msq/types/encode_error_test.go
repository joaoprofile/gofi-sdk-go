package types

import "testing"

func TestNewMessage_EncodingErrorIsReturned(t *testing.T) {
	msg, err := NewMessage(make(chan int))
	if err == nil || msg != nil {
		t.Fatalf("unencodable value must fail, got msg=%v err=%v", msg, err)
	}
}
