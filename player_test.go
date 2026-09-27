package main

import (
	"testing"
	"time"
)

// Тесты математики позиций плеера: позиция всегда абсолютная (от начала трека),
// длительность — всегда полная, перемотка её не урезает.
func TestStateAbsolutePosition(t *testing.T) {
	p := &player{volume: 0.8}
	p.duration = 120 * time.Second
	p.offset = 60 * time.Second // после перемотки в середину
	p.playing = true
	p.startedAt = time.Now().Add(-5 * time.Second)

	_, pos, dur, _ := p.State()
	if dur != 120*time.Second {
		t.Fatalf("duration = %v, want полная 120s (перемотка не урезает)", dur)
	}
	if pos < 64*time.Second || pos > 66*time.Second {
		t.Fatalf("pos = %v, want ~65s (offset 60s + 5s игры)", pos)
	}
}

func TestStatePausedMidChunk(t *testing.T) {
	p := &player{}
	p.duration = 120 * time.Second
	p.offset = 60 * time.Second
	p.playing = false
	p.pausedAt = 10 * time.Second // пауза на 10-й секунде чанка

	_, pos, dur, _ := p.State()
	if pos != 70*time.Second {
		t.Fatalf("pos = %v, want 70s (60s offset + 10s в чанке)", pos)
	}
	if dur != 120*time.Second {
		t.Fatalf("dur = %v", dur)
	}
}

func TestNaturalEndSetsPositionToEnd(t *testing.T) {
	// чанк 60..120 с дошёл до конца: позиция должна стать 120с, а не 60с (начало чанка)
	p := &player{}
	p.duration = 120 * time.Second
	p.offset = 60 * time.Second
	p.pausedAt = 30 * time.Second // играл 30с чанка до конца
	// эмулируем естественное завершение:pausedAt = duration-offset
	p.playing = false
	p.pausedAt = p.duration - p.offset

	_, pos, _, _ := p.State()
	if pos != 120*time.Second {
		t.Fatalf("pos = %v, want 120s (конец трека)", pos)
	}
}

func TestVolumeClamp(t *testing.T) {
	p := &player{}
	p.SetVolume(1.5)
	if p.volume != 1 {
		t.Fatalf("volume = %v, want clamp 1", p.volume)
	}
	p.SetVolume(-0.3)
	if p.volume != 0 {
		t.Fatalf("volume = %v, want clamp 0", p.volume)
	}
}
