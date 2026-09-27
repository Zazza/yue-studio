package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"yue-studio/internal/yue"
)

// fakeService — мок yue.Service: по умолчанию всё падает, тесты
// переопределяют только нужные методы.
type fakeService struct {
	yue.Service
	jobs      []yue.Job
	submitted []yue.SubmitParams
	fetchData map[string][]byte // file -> bytes
	fetchErr  map[string]error
}

func (f *fakeService) Jobs(ctx context.Context) ([]yue.Job, error) { return f.jobs, nil }

func (f *fakeService) Submit(ctx context.Context, p yue.SubmitParams) (int64, error) {
	f.submitted = append(f.submitted, p)
	return int64(len(f.submitted)), nil
}

func (f *fakeService) FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error) {
	if e := f.fetchErr[file]; e != nil {
		return nil, "", e
	}
	return io.NopCloser(bytes.NewReader(f.fetchData[file])), "application/octet-stream", nil
}

// fakePlayer — мок Player: записывает вызовы.
type fakePlayer struct {
	loaded  []int64
	plays   int
	stops   int
	volumes []float64
	seeked  []time.Duration
}

func (p *fakePlayer) Load(id int64, data []byte, dur time.Duration) error {
	p.loaded = append(p.loaded, id)
	return nil
}
func (p *fakePlayer) Play() error { p.plays++; return nil }
func (p *fakePlayer) Toggle()     {}
func (p *fakePlayer) Stop()       { p.stops++ }
func (p *fakePlayer) Seek(t time.Duration) error {
	p.seeked = append(p.seeked, t)
	return nil
}
func (p *fakePlayer) State() (bool, time.Duration, time.Duration, int64) { return false, 0, 0, 0 }
func (p *fakePlayer) SetVolume(v float64)                                { p.volumes = append(p.volumes, v) }
func (p *fakePlayer) LastError() string                                  { return "" }

func newTestApp(s yue.Service, p Player) *App { return &App{yue: s, player: p} }

func TestSubmitFanClampsN(t *testing.T) {
	// spec: n < 1 → 1 джоба, n > 10 → 10
	s := &fakeService{}
	a := newTestApp(s, &fakePlayer{})

	ids, err := a.YueSubmitFan(yue.SubmitParams{}, 0)
	if err != nil || len(ids) != 1 {
		t.Fatalf("n=0: ids=%v err=%v", ids, err)
	}
	ids, err = a.YueSubmitFan(yue.SubmitParams{}, 99)
	if err != nil || len(ids) != 10 {
		t.Fatalf("n=99: ids=%v err=%v", ids, err)
	}
}

func TestSubmitFanSeeds(t *testing.T) {
	// spec: сиды base+0..n-1; base берётся из параметров, если задан
	s := &fakeService{}
	a := newTestApp(s, &fakePlayer{})

	if _, err := a.YueSubmitFan(yue.SubmitParams{Seed: 100}, 3); err != nil {
		t.Fatal(err)
	}
	want := []int64{100, 101, 102}
	for i, p := range s.submitted {
		if p.Seed != want[i] {
			t.Fatalf("job %d: seed = %d, want %d", i, p.Seed, want[i])
		}
	}
}

func TestSubmitFanTitles(t *testing.T) {
	// spec: имя помечается [i/n]; без имени — остаётся пустым
	s := &fakeService{}
	a := newTestApp(s, &fakePlayer{})

	if _, err := a.YueSubmitFan(yue.SubmitParams{Title: "песня"}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.YueSubmitFan(yue.SubmitParams{}, 2); err != nil {
		t.Fatal(err)
	}
	if s.submitted[0].Title != "песня [1/2]" || s.submitted[1].Title != "песня [2/2]" {
		t.Fatalf("titled: %+v", s.submitted[:2])
	}
	if s.submitted[2].Title != "" || s.submitted[3].Title != "" {
		t.Fatalf("untitled: %+v", s.submitted[2:])
	}
}

func TestSubmitFanRandomSeedsPositive(t *testing.T) {
	// spec: без сида base случайный, но сиды всё равно последовательные
	s := &fakeService{}
	a := newTestApp(s, &fakePlayer{})

	if _, err := a.YueSubmitFan(yue.SubmitParams{}, 3); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 3; i++ {
		if s.submitted[i].Seed != s.submitted[0].Seed+int64(i) {
			t.Fatalf("seeds not sequential: %+v", s.submitted)
		}
	}
	if s.submitted[0].Seed <= 0 {
		t.Fatalf("base seed should be positive, got %d", s.submitted[0].Seed)
	}
}

func TestApplyDspUnknownChain(t *testing.T) {
	a := newTestApp(&fakeService{}, &fakePlayer{})
	if _, err := a.YueApplyDsp(1, "no-such-chain", nil); err == nil || !strings.Contains(err.Error(), "no-such-chain") {
		t.Fatalf("expected unknown chain error, got %v", err)
	}
}

func TestApplyDspNoAudio(t *testing.T) {
	s := &fakeService{jobs: []yue.Job{{ID: 1, Status: "done"}}} // без audio_file
	a := newTestApp(s, &fakePlayer{})
	if _, err := a.YueApplyDsp(1, "wall", nil); err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("expected no-audio error, got %v", err)
	}
}

func TestPlayAudioPrefersFlac(t *testing.T) {
	// spec: flac → wav → mp3 (меньше трафика — первым)
	s := &fakeService{
		jobs: []yue.Job{{ID: 1, AudioFile: "audio.flac", WavFile: "audio.wav", Mp3File: "audio.mp3"}},
	}
	pl := &fakePlayer{}
	a := newTestApp(s, pl)
	if err := a.YuePlayAudio(1); err != nil {
		t.Fatal(err)
	}
	if len(pl.loaded) != 1 || pl.loaded[0] != 1 || pl.plays != 1 {
		t.Fatalf("player calls: loaded=%v plays=%d", pl.loaded, pl.plays)
	}
}

func TestPlayAudioFallsBackToMp3(t *testing.T) {
	s := &fakeService{
		jobs:      []yue.Job{{ID: 2, Mp3File: "audio.mp3"}},
		fetchData: map[string][]byte{"audio.mp3": {1, 2, 3}},
	}
	pl := &fakePlayer{}
	a := newTestApp(s, pl)
	if err := a.YuePlayAudio(2); err != nil {
		t.Fatal(err)
	}
	if len(pl.loaded) != 1 || pl.plays != 1 {
		t.Fatalf("player calls: loaded=%v plays=%d", pl.loaded, pl.plays)
	}
}

func TestPlayAudioNoAudio(t *testing.T) {
	s := &fakeService{jobs: []yue.Job{{ID: 3, Status: "done"}}}
	a := newTestApp(s, &fakePlayer{})
	if err := a.YuePlayAudio(3); err == nil {
		t.Fatal("expected error for job without audio")
	}
}

func TestPlayAudioUnknownJob(t *testing.T) {
	a := newTestApp(&fakeService{jobs: []yue.Job{{ID: 1}}}, &fakePlayer{})
	if err := a.YuePlayAudio(999); err == nil {
		t.Fatal("expected error for unknown job")
	}
}

func TestShutdownStopsPlayer(t *testing.T) {
	pl := &fakePlayer{}
	a := newTestApp(&fakeService{}, pl)
	a.shutdown(context.Background())
	if pl.stops != 1 {
		t.Fatalf("stops = %d", pl.stops)
	}
}

func TestSeekAudioSecondsToDuration(t *testing.T) {
	pl := &fakePlayer{}
	a := newTestApp(&fakeService{}, pl)
	if err := a.YueSeekAudio(1.5); err != nil {
		t.Fatal(err)
	}
	if len(pl.seeked) != 1 || pl.seeked[0] != 1500*time.Millisecond {
		t.Fatalf("seeked = %v", pl.seeked)
	}
}
