package postgres

import (
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadPool распределяет read-запросы round-robin по пулам реплик.
// С одним пулом (реплики не настроены) всегда возвращает его.
type ReadPool struct {
	pools []*pgxpool.Pool
	next  atomic.Uint64
}

func NewReadPool(pools []*pgxpool.Pool) *ReadPool {
	return &ReadPool{pools: pools}
}

func (p *ReadPool) Next() *pgxpool.Pool {
	return p.pools[p.next.Add(1)%uint64(len(p.pools))]
}
