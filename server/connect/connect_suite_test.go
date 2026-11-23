package connect

import (
	"testing"

	"github.com/navidrome/navidrome/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// No tests.Init: nothing here touches the database, so the suite also runs without CGO.
func TestConnect(t *testing.T) {
	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Connect Suite")
}
