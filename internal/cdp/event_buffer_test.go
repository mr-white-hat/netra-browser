package cdp

import (
	"strconv"
	"testing"
	"time"
)

func TestRingBufferRecentByType(t *testing.T) {
	b := NewRingBuffer(100)
	b.Add(BufferedEvent{At: time.UnixMilli(1), Method: "Page.frameNavigated"})
	b.Add(BufferedEvent{At: time.UnixMilli(2), Method: "Network.requestWillBeSent"})
	b.Add(BufferedEvent{At: time.UnixMilli(3), Method: "Page.frameNavigated"})

	got := b.Recent(time.UnixMilli(0), []string{"Page.frameNavigated"})
	if len(got) != 2 {
		t.Fatalf("want 2 page events, got %d", len(got))
	}
}

func TestRingBufferDropsOldest(t *testing.T) {
	b := NewRingBuffer(3)
	for i := 0; i < 5; i++ {
		b.Add(BufferedEvent{At: time.UnixMilli(int64(i)), Method: "x" + strconv.Itoa(i)})
	}
	all := b.Recent(time.Time{}, nil)
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
	if all[0].Method != "x2" {
		t.Fatalf("oldest should be x2, got %s", all[0].Method)
	}
}

func TestRingBufferFiltersBySince(t *testing.T) {
	b := NewRingBuffer(10)
	b.Add(BufferedEvent{At: time.UnixMilli(1), Method: "a"})
	b.Add(BufferedEvent{At: time.UnixMilli(5), Method: "b"})
	b.Add(BufferedEvent{At: time.UnixMilli(10), Method: "c"})
	got := b.Recent(time.UnixMilli(5), nil)
	if len(got) != 2 || got[0].Method != "b" {
		t.Fatalf("got %+v", got)
	}
}
