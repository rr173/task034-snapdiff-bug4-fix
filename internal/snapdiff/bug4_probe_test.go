package snapdiff

import (
	"strconv"
	"sync"
	"testing"
)

func TestBug4StoreConcurrentPutGet(t *testing.T) {
	store := NewStore()
	snap, err := NewSnapshot([]FileInput{{Path: "f.txt", Content: "value"}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				id := "snapshot-" + strconv.Itoa(i%16)
				if worker%2 == 0 {
					store.Put(id, snap)
				} else {
					store.Get(id)
				}
			}
		}()
	}
	wg.Wait()
}
