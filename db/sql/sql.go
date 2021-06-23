package sql

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/iden3/go-merkletree"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// TODO: upsert or insert?
const upsertStmt = `INSERT INTO mt_nodes (mt_id, key, type, child_l, child_r, entry) VALUES ($1, $2, $3, $4, $5, $6) ` +
	`ON CONFLICT (mt_id, key) DO UPDATE SET type = $3, child_l = $4, child_r = $5, entry = $6`

const updateRootStmt = `INSERT INTO mt_roots (mt_id, key) VALUES ($1, $2) ` +
	`ON CONFLICT (mt_id) DO UPDATE SET key = $2`

// Storage implements the db.Storage interface
type Storage struct {
	db             *sqlx.DB
	mtId           uint64
	currentVersion uint64
	currentRoot    *merkletree.Hash
}

// StorageTx implements the db.Tx interface
type StorageTx struct {
	*Storage
	tx          *sqlx.Tx
	cache       merkletree.KvMap
	currentRoot *merkletree.Hash
}

type NodeItem struct {
	MTId uint64 `db:"mt_id"`
	Key  []byte `db:"key"`
	// Type is the type of node in the tree.
	Type byte `db:"type"`
	// ChildL is the left child of a middle node.
	ChildL []byte `db:"child_l"`
	// ChildR is the right child of a middle node.
	ChildR []byte `db:"child_r"`
	// Entry is the data stored in a leaf node.
	Entry     []byte  `db:"entry"`
	CreatedAt *uint64 `db:"created_at"`
	DeletedAt *uint64 `db:"deleted_at"`
}

type RootItem struct {
	MTId      uint64  `db:"mt_id"`
	Key       []byte  `db:"key"`
	CreatedAt *uint64 `db:"created_at"`
	DeletedAt *uint64 `db:"deleted_at"`
}

// NewSqlStorage returns a new Storage
func NewSqlStorage(db *sqlx.DB, errorIfMissing bool) (*Storage, error) {
	return &Storage{db: db}, nil
}

// WithPrefix implements the method WithPrefix of the interface db.Storage
func (s *Storage) WithPrefix(prefix []byte) merkletree.Storage {
	//return &Storage{db: s.db, prefix: merkletree.Concat(s.prefix, prefix)}
	// TODO: remove WithPrefix method
	mtId := s.mtId<<4 | binary.LittleEndian.Uint64(prefix)
	return &Storage{db: s.db, mtId: mtId}
}

// NewTx implements the method NewTx of the interface db.Storage
func (s *Storage) NewTx() (merkletree.Tx, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return nil, err
	}
	return &StorageTx{s, tx, make(merkletree.KvMap), s.currentRoot}, nil
}

// Get retrieves a value from a key in the db.Storage
func (s *Storage) Get(key []byte) (*merkletree.Node, error) {
	item := NodeItem{}
	err := s.db.Get(&item, "SELECT * FROM mt_nodes WHERE mt_id = $1 AND key = $2", s.mtId, key)
	if err == sql.ErrNoRows {
		return nil, merkletree.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	node, err := item.Node()
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetRoot retrieves a merkle tree root hash in the interface db.Tx
func (s *Storage) GetRoot() (*merkletree.Hash, error) {
	var root merkletree.Hash

	if s.currentRoot != nil {
		copy(root[:], s.currentRoot[:])
		return &root, nil
	}

	item := RootItem{}
	err := s.db.Get(&item, "SELECT * FROM mt_roots WHERE mt_id = $1", s.mtId)
	if err == sql.ErrNoRows {
		return nil, merkletree.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	copy(root[:], item.Key[:])
	return &root, nil
}

// Iterate implements the method Iterate of the interface db.Storage
func (s *Storage) Iterate(f func([]byte, *merkletree.Node) (bool, error)) error {
	items := []NodeItem{}

	err := s.db.Select(&items, "SELECT * FROM mt_nodes WHERE key WHERE mt_id = $1", s.mtId)
	if err != nil {
		return err
	}
	for _, v := range items {
		k := v.Key[:]
		n, err := v.Node()
		if err != nil {
			return err
		}
		cont, err := f(k, n)
		if err != nil {
			return err
		}
		if !cont {
			break
		}
	}
	return nil
}

// Get retrieves a value from a key in the interface db.Tx
func (tx *StorageTx) Get(key []byte) (*merkletree.Node, error) {
	//fullKey := append(tx.mtId, key...)
	fullKey := key
	if value, ok := tx.cache.Get(fullKey); ok {
		return &value, nil
	}

	item := NodeItem{}
	err := tx.tx.Get(&item, "SELECT * FROM mt_nodes WHERE mt_id = $1 AND key = $2", tx.mtId, key)
	if err == sql.ErrNoRows {
		return nil, merkletree.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	node, err := item.Node()
	if err != nil {
		return nil, err
	}
	return node, nil
}

// Put saves a key:value into the db.Storage
func (tx *StorageTx) Put(k []byte, v *merkletree.Node) error {
	//fullKey := append(tx.mtId, k...)
	fullKey := k
	tx.cache.Put(fullKey, *v)
	fmt.Printf("tx.Put(%x, %+v)\n", fullKey, v)
	return nil
}

// GetRoot retrieves a merkle tree root hash in the interface db.Tx
func (tx *StorageTx) GetRoot() (*merkletree.Hash, error) {
	var root merkletree.Hash

	if tx.currentRoot != nil {
		copy(root[:], tx.currentRoot[:])
		return &root, nil
	}

	item := RootItem{}
	err := tx.tx.Get(&item, "SELECT * FROM mt_roots WHERE mt_id = $1", tx.mtId)
	if err == sql.ErrNoRows {
		return nil, merkletree.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	copy(root[:], item.Key[:])
	return &root, nil
}

// SetRoot sets a hash of merkle tree root in the interface db.Tx
func (tx *StorageTx) SetRoot(hash *merkletree.Hash) error {
	root := &merkletree.Hash{}
	copy(root[:], hash[:])
	tx.currentRoot = root
	return nil
}

// Add implements the method Add of the interface db.Tx
func (tx *StorageTx) Add(atx merkletree.Tx) error {
	dbtx := atx.(*StorageTx)
	//if !bytes.Equal(tx.prefix, dbtx.prefix) {
	//	// TODO: change cache to store prefix too!
	//	return errors.New("adding StorageTx with different prefix is not implemented")
	//}
	if tx.mtId != dbtx.mtId {
		// TODO: change cache to store prefix too!
		return errors.New("adding StorageTx with different prefix is not implemented")
	}
	for _, v := range dbtx.cache {
		tx.cache.Put(v.K, v.V)
	}
	tx.currentRoot = dbtx.currentRoot
	return nil
}

// Commit implements the method Commit of the interface db.Tx
func (tx *StorageTx) Commit() error {
	// execute a query on the server
	fmt.Printf("Commit\n")
	for _, v := range tx.cache {
		fmt.Printf("key %x, value %+v\n", v.K, v.V)
		node := v.V

		var childL []byte
		if node.ChildL != nil {
			childL = append(childL, node.ChildL[:]...)
		}

		var childR []byte
		if node.ChildR != nil {
			childR = append(childR, node.ChildR[:]...)
		}

		var entry []byte
		if node.Entry[0] != nil && node.Entry[1] != nil {
			entry = append(node.Entry[0][:], node.Entry[1][:]...)
		}

		key, err := node.Key()
		if err != nil {
			return err
		}
		_, err = tx.tx.Exec(upsertStmt, tx.mtId, key[:], node.Type, childL, childR, entry)
		if err != nil {
			return err
		}
	}

	_, err := tx.tx.Exec(updateRootStmt, tx.mtId, tx.currentRoot[:])
	if err != nil {
		return err
	}

	tx.cache = nil
	return tx.tx.Commit()
}

// Close implements the method Close of the interface db.Tx
func (tx *StorageTx) Close() {
	//tx.tx.Rollback()
	tx.cache = nil
}

// Close implements the method Close of the interface db.Storage
func (s *Storage) Close() {
	err := s.db.Close()
	if err != nil {
		panic(err)
	}
}

// List implements the method List of the interface db.Storage
func (s *Storage) List(limit int) ([]merkletree.KV, error) {
	ret := []merkletree.KV{}
	err := s.Iterate(func(key []byte, value *merkletree.Node) (bool, error) {
		ret = append(ret, merkletree.KV{K: merkletree.Clone(key), V: *value})
		if len(ret) == limit {
			return false, nil
		}
		return true, nil
	})
	return ret, err
}

func (item *NodeItem) Node() (*merkletree.Node, error) {
	node := merkletree.Node{
		Type: merkletree.NodeType(item.Type),
	}
	if item.ChildL != nil {
		node.ChildL = &merkletree.Hash{}
		copy(node.ChildL[:], item.ChildL[:])
	}
	if item.ChildR != nil {
		node.ChildR = &merkletree.Hash{}
		copy(node.ChildR[:], item.ChildR[:])
	}
	if len(item.Entry) > 0 {
		if len(item.Entry) != 2*merkletree.ElemBytesLen {
			return nil, merkletree.ErrNodeBytesBadSize
		}
		node.Entry = [2]*merkletree.Hash{{}, {}}
		copy(node.Entry[0][:], item.Entry[0:32])
		copy(node.Entry[1][:], item.Entry[32:64])
	}
	return &node, nil
}