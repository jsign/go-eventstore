package main

import (
	"testing"

	"github.com/textileio/go-eventstore/store"
)

func TestBooks(t *testing.T) {
	s := createMemStore()

	model, err := s.RegisterJSONPatcher("Book", &Book{})
	checkErr(err)

	// Bootstrap the model with some books: two from Author1 and one from Author2
	{
		// Create a book with two comments
		book1 := &Book{ // Notice ID will be autogenerated
			Title:  "Title1",
			Author: "Author1",
			Comments: []Comment{
				Comment{
					Author: "AuthorComment1",
					Body:   "This book is great!",
					Rating: 4,
				},
				Comment{
					Author: "AuthorComment2",
					Body:   "Highly recommend this book!",
					Rating: 5,
				},
			},
		}

		// Create the book in the model
		err = model.Create(book1)
		checkErr(err)

		// Add some extra comment and save
		book1.Comments = append(book1.Comments, Comment{Author: "AuthorComment3", Body: "This book is terrible", Rating: 1})
		model.Save(book1)

		// Create other books without comments
		book2 := &Book{
			Title:  "Title2",
			Author: "Author2",
		}
		checkErr(model.Create(book2))

		// Create other book from Author1
		book3 := &Book{
			Title:  "Title3",
			Author: "Author1",
		}
		checkErr(model.Create(book3))

	}

	// Query, Update, and Save
	{
		var books []*Book
		err := model.Find(&books, store.Where("Title").Eq("Title3"))
		checkErr(err)

		// Modify title
		book := books[0]
		book.Title = "ModifiedTitle"
		model.Save(book)
		err = model.Find(&books, store.Where("Title").Eq("Title3"))
		checkErr(err)
		if len(books) != 0 {
			panic("Book with Title3 shouldn't exist")
		}

		// Delete it
		err = model.Find(&books, store.Where("Title").Eq("ModifiedTitle"))
		checkErr(err)
		if len(books) != 1 {
			panic("Book with ModifiedTitle should exist")
		}
		model.Delete(books[0].ID)
		err = model.Find(&books, store.Where("Title").Eq("ModifiedTitle"))
		checkErr(err)
		if len(books) != 0 {
			panic("Book with ModifiedTitle shouldn't exist")
		}
	}

}
