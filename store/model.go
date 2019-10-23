package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/alecthomas/jsonschema"
	jsonpatch "github.com/evanphx/json-patch"
	ds "github.com/ipfs/go-datastore"
	"github.com/textileio/go-eventstore"
)

var (
	ErrNotFound   = errors.New("instance not found")
	ErrReadonlyTx = errors.New("read only transaction")
)

type operationType string

const (
	upsert operationType = "upsert"
	delete operationType = "delete"
)

type Model struct {
	lock            sync.RWMutex
	schema          *jsonschema.Schema
	valueType       reflect.Type
	datastore       ds.Datastore
	dispatcher      *eventstore.Dispatcher
	dispatcherToken eventstore.Token
}

func (m *Model) Read(f func(txn *Txn) error) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	txn := &Txn{model: m, readonly: true, ops: make(map[ds.Key]operation)}
	defer txn.Discard()
	if err := f(txn); err != nil {
		return err
	}
	return nil
}

func (m *Model) Update(f func(txn *Txn) error) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	txn := &Txn{model: m, ops: make(map[ds.Key]operation)}
	defer txn.Discard()
	if err := f(txn); err != nil {
		return err
	}
	return txn.Commit()
}

func (m *Model) FindByID(id string, v interface{}) error {
	key := ds.NewKey(id)
	bytes, err := m.datastore.Get(key)
	if errors.Is(err, ds.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, v)
}

func (m *Model) Reduce(event eventstore.Event) (err error) {
	log.Debugf("reducer %s start", m.schema.Ref)
	defer log.Debugf("reducer %s end with err: %v", m.schema.Ref, err)
	if event.Type() != m.schema.Ref {
		log.Debugf("ignoring event from uninteresting type")
		return nil
	}
	var op operation
	err = json.Unmarshal(event.Body(), &op)
	if err != nil {
		return
	}

	key := ds.NewKey(event.EntityID())
	switch op.Type {
	case upsert:
		var value, patchedValue []byte
		value, err = m.datastore.Get(key)
		if errors.Is(err, ds.ErrNotFound) {
			if err = m.datastore.Put(key, op.JSONPatch); err != nil {
				return
			}
			log.Debug("\tinsert operation applied")
			return
		}
		if err != nil {
			return
		}
		patchedValue, err = jsonpatch.MergePatch(value, op.JSONPatch)
		if err != nil {
			return fmt.Errorf("error when patching value: %v", err)
		}
		if err = m.datastore.Put(key, patchedValue); err != nil {
			return
		}
		log.Debug("\tupdate operation applied")
	case delete:
		if err = m.datastore.Delete(key); err != nil {
			return
		}
		log.Debug("\tdelete operation applied")
	default:
		return fmt.Errorf("unknown operation %s", op.Type)
	}

	return nil
}

type Txn struct {
	model     *Model
	discarded bool
	commited  bool
	readonly  bool
	ops       map[ds.Key]operation
}

type operation struct {
	Type      operationType
	EntityID  string
	JSONPatch []byte
}

func (t *Txn) Discard() {
	t.discarded = true
}

func (t *Txn) Commit() error {
	if t.discarded || t.commited {
		return fmt.Errorf("can't commit discarded/commited txn")
	}
	now := time.Now()
	//  ToDo/Important: As first approximation, each key change is a separate event
	for _, op := range t.ops {
		opBytes, err := json.Marshal(op)
		if err != nil {
			return err
		}
		event := eventstore.NewJsonPatchEvent(now, op.EntityID, t.model.schema.Ref, opBytes)
		if err := t.model.dispatcher.Dispatch(event); err != nil {
			return err // Ugh! partial failure, think about what this means for application state
		}
	}
	return nil
}

func (t *Txn) Add(id string, new interface{}) error {
	if t.readonly {
		return ErrReadonlyTx
	}
	key := ds.NewKey(id)
	exists, err := t.model.datastore.Has(key)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("can't add already existing instance id:%s", id)
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return err
	}
	t.ops[key] = operation{Type: upsert, EntityID: id, JSONPatch: newBytes}
	return nil
}

func (t *Txn) Save(id string, updated interface{}) error {
	if t.readonly {
		return ErrReadonlyTx
	}

	key := ds.NewKey(id)
	actual, err := t.model.datastore.Get(key)
	if err == ds.ErrNotFound {
		return fmt.Errorf("can't save unkown instance id:%s", id)
	}
	if err != nil {
		return err
	}
	newBytes, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	jsonPatch, err := jsonpatch.CreateMergePatch(actual, newBytes)
	if err != nil {
		return err
	}
	t.ops[key] = operation{Type: upsert, EntityID: id, JSONPatch: jsonPatch}
	return nil
}

func (t *Txn) Delete(id string) error {
	key := ds.NewKey(id)
	exists, err := t.model.datastore.Has(key)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("can't remove since doesn't exist: %s", id)
	}
	t.ops[key] = operation{Type: delete, EntityID: id}
	return nil
}

func (t *Txn) FindByID(id string, v interface{}) error {
	return t.model.FindByID(id, v)
}