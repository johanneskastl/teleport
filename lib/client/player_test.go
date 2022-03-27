package client

import (
	"bytes"
	"testing"
	"time"

	"github.com/gravitational/teleport/lib/client/terminal"
	"github.com/gravitational/teleport/lib/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func testTerm(t *testing.T) *terminal.Terminal {
	t.Helper()
	term, err := terminal.New(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, term.Close())
	})
	return term
}

// TestEmptyPlay verifies that a playback of 0 events
// immediately transitions to a stopped state.
func TestEmptyPlay(t *testing.T) {
	c := clockwork.NewFakeClock()
	p := newSessionPlayer(nil, nil, testTerm(t))
	p.clock = c

	p.Play()

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for player to complete")
	case <-p.stopC:
	}

	require.True(t, p.Stopped())
}

// TestPlayPause verifies the play/pause functionality.
func TestPlayPause(t *testing.T) {
	c := clockwork.NewFakeClock()

	// in this test, we let the player play 2 of the 3 events,
	// then pause it and verify the pause state before resuming
	// playback for the final event.
	events := []events.EventFields{
		{
			events.EventType: events.SessionPrintEvent,
			"ms":             100,
		},
		{
			events.EventType: events.SessionPrintEvent,
			"ms":             200,
		},
		{
			events.EventType: events.SessionPrintEvent,
			"ms":             300,
		},
	}
	var stream []byte // intentionally empty, we dont care about stream contents here
	p := newSessionPlayer(events, stream, testTerm(t))
	p.clock = c

	p.Play()

	// wait for player to see the first event and apply the delay
	c.BlockUntil(1)

	// advance the clock:
	// at this point, the player will write the first event
	c.Advance(100 * time.Millisecond)

	// wait for the player to sleep on the 2nd event
	c.BlockUntil(1)

	// pause playback
	// note: we don't use p.TogglePause here, as it waits for the state transition,
	// and the state won't transition proceed until we advance the clock
	p.Lock()
	p.setState(stateStopping)
	p.Unlock()

	// advance the clock again:
	// the player will write the second event and
	// then realize that it's been asked to pause
	c.Advance(101 * time.Millisecond)

	p.Lock()
	p.waitUntil(stateStopped)
	p.Unlock()

	ch := make(chan struct{})
	go func() {
		// resume playback
		p.TogglePause()
		ch <- struct{}{}
	}()

	// playback should resume for the 3rd and final event:
	// in this case, the first two events are written immediately without delay,
	// and we block here until the player is sleeping prior to the 3rd event
	c.BlockUntil(1)

	// make sure that we've resumed
	<-ch
	require.False(t, p.Stopped())

	// advance the clock a final time, forcing the player to write the last event
	// note: on the resume, we play the succesful events immediately, and then sleep
	// up to the resume point, which is why we advance by 300ms here
	c.Advance(300 * time.Millisecond)

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for player to complete")
	case <-p.stopC:
	}
	require.True(t, p.Stopped())
}
