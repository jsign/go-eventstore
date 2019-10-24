package store

import (
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	ds "github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log"
	"github.com/textileio/go-eventstore"
)

const (
	errInvalidInstanceState = "invalid instance state"
)

type Person struct {
	ID   string
	Name string
	Age  int
}

type Dog struct {
	ID       string
	Name     string
	Comments []Comment
}
type Comment struct {
	Body string
}

func TestMain(m *testing.M) {
	logging.SetLogLevel("*", "debug")
	os.Exit(m.Run())
}

func TestSchemaRegistration(t *testing.T) {
	t.Parallel()

	t.Run("Single", func(t *testing.T) {
		t.Parallel()
		store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
		_, err := store.Register("Dog", &Dog{})
		checkErr(t, err)
	})
	t.Run("Multiple", func(t *testing.T) {
		t.Parallel()
		store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
		_, err := store.Register("Dog", &Dog{})
		checkErr(t, err)
		_, err = store.Register("Person", &Person{})
		checkErr(t, err)
		// ToDo: Makes sense some api for store.GetModels()?
		// if that's the case, can be used here to assert registrations
	})
}

func TestAddInstance(t *testing.T) {
	t.Parallel()

	t.Run("Single", func(t *testing.T) {
		t.Parallel()

		store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
		model, err := store.Register("Person", &Person{})
		checkErr(t, err)

		t.Run("WithImplicitTx", func(t *testing.T) {
			newPerson := &Person{ID: uuid.New().String(), Name: "Foo", Age: 42}
			err = model.Add(newPerson.ID, newPerson)
			checkErr(t, err)
			assertPersonInModel(t, model, newPerson)
		})
		t.Run("WithTx", func(t *testing.T) {
			newPerson := &Person{ID: uuid.New().String(), Name: "Foo", Age: 42}
			err = model.Update(func(txn *Txn) error {
				return txn.Add(newPerson.ID, newPerson)
			})
			checkErr(t, err)
			assertPersonInModel(t, model, newPerson)
		})
	})
	t.Run("Multiple", func(t *testing.T) {
		t.Parallel()
		store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
		model, err := store.Register("Person", &Person{})
		checkErr(t, err)

		newPerson1 := &Person{ID: uuid.New().String(), Name: "Foo1", Age: 42}
		newPerson2 := &Person{ID: uuid.New().String(), Name: "Foo2", Age: 43}
		err = model.Update(func(txn *Txn) error {
			err := txn.Add(newPerson1.ID, newPerson1)
			if err != nil {
				return err
			}
			return txn.Add(newPerson2.ID, newPerson2)
		})
		checkErr(t, err)
		assertPersonInModel(t, model, newPerson1)
		assertPersonInModel(t, model, newPerson2)
	})
}

func TestGetInstance(t *testing.T) {
	t.Parallel()

	store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
	model, err := store.Register("Person", &Person{})
	checkErr(t, err)

	newPerson := &Person{ID: uuid.New().String(), Name: "Foo", Age: 42}
	err = model.Update(func(txn *Txn) error {
		return txn.Add(newPerson.ID, newPerson)
	})
	checkErr(t, err)

	t.Run("WithImplicitTx", func(t *testing.T) {
		person := &Person{}
		err = model.FindByID(newPerson.ID, person)
		checkErr(t, err)
		if !reflect.DeepEqual(newPerson, person) {
			t.Fatalf(errInvalidInstanceState)
		}
	})
	t.Run("WithReadTx", func(t *testing.T) {
		person := &Person{}
		err = model.Read(func(txn *Txn) error {
			txn.FindByID(newPerson.ID, person)
			checkErr(t, err)
			if !reflect.DeepEqual(newPerson, person) {
				t.Fatalf(errInvalidInstanceState)
			}
			return nil
		})
	})
	t.Run("WithUpdateTx", func(t *testing.T) {
		person := &Person{}
		err = model.Update(func(txn *Txn) error {
			txn.FindByID(newPerson.ID, person)
			checkErr(t, err)
			if !reflect.DeepEqual(newPerson, person) {
				t.Fatalf(errInvalidInstanceState)
			}
			return nil
		})
	})
}

func TestUpdateInstance(t *testing.T) {
	t.Parallel()

	store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
	model, err := store.Register("Person", &Person{})
	checkErr(t, err)

	id := uuid.New().String()
	err = model.Update(func(txn *Txn) error {
		newPerson := &Person{ID: id, Name: "Alice", Age: 42}
		return txn.Add(newPerson.ID, newPerson)
	})
	checkErr(t, err)

	err = model.Update(func(txn *Txn) error {
		p := &Person{}
		err := txn.FindByID(id, p)
		checkErr(t, err)

		p.Name = "Bob"
		return txn.Save(p.ID, p)
	})
	checkErr(t, err)

	// Under the hood here the instance update went through
	// the dispatcher, then the reducer, which will ultimately
	// apply the change to the current instance state that
	// should make the code below behave as expected

	person := &Person{}
	err = model.FindByID(id, person)
	checkErr(t, err)
	if person.ID != id || person.Age != 42 || person.Name != "Bob" {
		t.Fatalf(errInvalidInstanceState)
	}
}

func TestDeleteInstance(t *testing.T) {
	t.Parallel()

	store := NewStore(ds.NewMapDatastore(), eventstore.NewDispatcher(eventstore.NewTxMapDatastore()))
	model, err := store.Register("Person", &Person{})
	checkErr(t, err)

	id := uuid.New().String()
	err = model.Update(func(txn *Txn) error {
		newPerson := &Person{ID: id, Name: "Alice", Age: 42}
		return txn.Add(newPerson.ID, newPerson)
	})
	checkErr(t, err)

	err = model.Delete(id)
	checkErr(t, err)

	if err = model.FindByID(id, &Person{}); err != ErrNotFound {
		t.Fatalf("FindByID: instance shouldn't exist")
	}
	if exist, err := model.Has(id); exist || err != nil {
		t.Fatalf("Has: instance shouldn't exist")
	}

	// Try to delete again
	if err = model.Delete(id); err != ErrNotFound {
		t.Fatalf("cant't delete non-existent instance")
	}
}

// ToDo
func TestInvalidActions(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		// Compared to schema
	})
	t.Run("Update", func(t *testing.T) {
		// Compared to schema
	})
}

func checkErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertPersonInModel(t *testing.T, model *Model, person *Person) {
	p := &Person{}
	err := model.FindByID(person.ID, p)
	checkErr(t, err)
	if !reflect.DeepEqual(person, p) {
		t.Fatalf(errInvalidInstanceState)
	}
}
