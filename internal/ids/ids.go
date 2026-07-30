// Package ids mints the server-assigned identifiers the domain requires.
//
// Identity is server-owned: a client never supplies a reservation or booking identifier
// (domain.IDGen). This is the production implementation of that port; tests use
// deterministic sequential generators instead, which is why the interface exists.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Random mints identifiers from crypto/rand. The zero value is ready to use and is safe
// for concurrent use.
//
// Random rather than sequential because these identifiers are handed to clients, and a
// guessable one would let a caller name another's reservation on confirm or cancel. AG-M1
// has no authentication, so unguessability is the only thing standing between a booking
// and a stranger — which is a named gap, not a substitute for the authorisation check a
// later milestone must add.
type Random struct{}

// prefixes distinguish the two identifier kinds on sight, in a log or a support
// conversation. They carry no meaning to the system, which treats identifiers as opaque.
const (
	reservationPrefix = "res_"
	bookingPrefix     = "bk_"
	// entropyBytes is 16 bytes = 128 bits, the same order as a UUIDv4, without taking
	// on a dependency to format one.
	entropyBytes = 16
)

func (Random) NewReservationID() domain.ReservationID {
	return domain.ReservationID(reservationPrefix + token())
}

func (Random) NewBookingID() domain.BookingID {
	return domain.BookingID(bookingPrefix + token())
}

// token returns hex-encoded entropy.
//
// It panics if the system entropy source fails, deliberately: returning an error would
// push a "cannot happen" branch into every call site, and falling back to a weaker source
// would silently produce the guessable identifiers this type exists to prevent. A system
// where crypto/rand.Read fails must not be minting booking identifiers.
func token() string {
	var b [entropyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("ids: system entropy unavailable: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Static assertion that Random satisfies the domain port.
var _ domain.IDGen = Random{}
