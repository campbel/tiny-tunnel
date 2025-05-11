package safe

import "sync"

type Map[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]V
}

func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		m: make(map[K]V),
	}
}

func (m *Map[K, V]) Get(k K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[k]
	return v, ok
}

func (m *Map[K, V]) SetNX(k K, v V) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.m[k]; ok {
		return false
	}
	m.m[k] = v
	return true
}

func (m *Map[K, V]) Delete(k K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, k)
}

// Range calls f sequentially for each key and value in the map.
// If f returns false, range stops the iteration.
// This method acquires a lock for the entire iteration to ensure consistency.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k, v := range m.m {
		if !f(k, v) {
			break
		}
	}
}

// Clear removes all entries from the map
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// The most efficient way to clear a map is to recreate it
	m.m = make(map[K]V)
}
