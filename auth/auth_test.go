package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRefreshToken(t *testing.T) {
	runAt(time.Now().Add(time.Duration(1)*time.Hour), func() {
		refreshToken()
	})
}

func TestRefresh_ExpectingRefresh(t *testing.T) {
	now := time.Now().Add(time.Duration(1) * time.Hour)
	runAt(now, func() {
		refreshThreshold := now.Add(time.Duration(-5) * time.Minute).Unix()
		assert.Equal(t, true, refresh(refreshThreshold))
	})
}

func TestRefresh_ExpectingNotRefresh(t *testing.T) {
	now := time.Now()
	runAt(now, func() {
		oneHourAhead := now.Add(time.Duration(1) * time.Hour)
		refreshThreshold := oneHourAhead.Add(time.Duration(-5) * time.Minute).Unix()
		assert.Equal(t, false, refresh(refreshThreshold))
	})
}

func runAt(t time.Time, fn func()) {
	timeFunc = func() time.Time {
		return t
	}
	fn()
	timeFunc = time.Now
}
