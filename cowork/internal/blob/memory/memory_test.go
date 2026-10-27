package memory_test

import (
	"testing"

	"github.com/garamsh/garam/cowork/internal/blob/blobtest"
	"github.com/garamsh/garam/cowork/internal/blob/memory"
)

func TestStore_KeepsTheContract(t *testing.T) { blobtest.Run(t, memory.New()) }
