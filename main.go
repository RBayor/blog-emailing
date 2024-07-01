package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/gomail.v2"
)

type Subscriber struct {
	ID           int    `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	SubscribedAt string `json:"subscribed_at"`
}

type Article struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	PublishedAt string `json:"published_at"`
}

type SentEmail struct {
	ID           int    `json:"id"`
	SubscriberID int    `json:"subscriber_id"`
	ArticleID    int    `json:"article_id"`
	SentAt       string `json:"sent_at"`
}

type AllData struct {
	Subscribers     []Subscriber `json:"subscribers"`
	SubscriberCount int          `json:"subscriber_count"`
	Articles        []Article    `json:"articles"`
	ArticleCount    int          `json:"article_count"`
	SentEmails      []SentEmail  `json:"sent_emails"`
	SentEmailCount  int          `json:"sent_email_count"`
}

const dbPath = "/data/blog.db"

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, using environment variables")
	}

	log.Printf("Attempting to open database at: %s", dbPath)
	// Set up database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// dropTables(db)
	// Create tables if not exist
	createTables(db)

	http.HandleFunc("/api/subscribe", handleSubscribe(db))
	http.HandleFunc("/api/publish", handlePublish(db))
	http.HandleFunc("/api/send-newsletter", handleSendNewsletter(db))
	http.HandleFunc("/api/stats", handleGetAllData(db))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func createTables(db *sql.DB) {
	log.Println("creating tables...")
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS subscribers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			name TEXT,
			subscribed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			published_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS sent_emails (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscriber_id INTEGER,
			article_id INTEGER,
			sent_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (subscriber_id) REFERENCES subscribers(id),
			FOREIGN KEY (article_id) REFERENCES articles(id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Tables successfully created!")
}

// func dropTables(db *sql.DB) {
// 	log.Println("dropping tables...")
// 	_, err := db.Exec(`
// 		DROP TABLE IF EXISTS sent_emails;
// 		DROP TABLE IF EXISTS articles;
// 		DROP TABLE IF EXISTS subscribers;
// 	`)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	log.Println("All tables dropped!")
// }

func handleSubscribe(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var sub Subscriber
		err := json.NewDecoder(r.Body).Decode(&sub)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = db.Exec("INSERT INTO subscribers (email, name) VALUES (?, ?)", sub.Email, sub.Name)
		if err != nil {
			http.Error(w, "Error subscribing", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Subscribed successfully"))
	}
}

func handlePublish(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var article Article
		err := json.NewDecoder(r.Body).Decode(&article)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result, err := db.Exec("INSERT INTO articles (title, content) VALUES (?, ?)", article.Title, article.Content)
		if err != nil {
			http.Error(w, "Error publishing article", http.StatusInternalServerError)
			return
		}

		articleID, _ := result.LastInsertId()

		// Trigger newsletter sending
		go sendNewsletterForArticle(db, int(articleID))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Article published successfully"))
	}
}

func handleSendNewsletter(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ArticleID int `json:"article_id"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		go sendNewsletterForArticle(db, req.ArticleID)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Newsletter sending triggered"))
	}
}

func sendNewsletterForArticle(db *sql.DB, articleID int) {
	log.Println("sending blog post")
	article, err := getArticle(db, articleID)
	if err != nil {
		log.Printf("Error getting article: %v", err)
		return
	}

	subscribers, err := getSubscribers(db)
	if err != nil {
		log.Printf("Error getting subscribers: %v", err)
		return
	}

	for _, sub := range subscribers {
		if !hasReceivedArticle(db, sub.ID, articleID) {
			if sendEmail(sub, article) {
				markEmailSent(db, sub.ID, articleID)
			}
		}
	}
}

func getArticle(db *sql.DB, id int) (Article, error) {
	var article Article
	err := db.QueryRow("SELECT id, title, content, published_at FROM articles WHERE id = ?", id).Scan(
		&article.ID, &article.Title, &article.Content, &article.PublishedAt)
	return article, err
}

func getSubscribers(db *sql.DB) ([]Subscriber, error) {
	rows, err := db.Query("SELECT id, email, name FROM subscribers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subscribers []Subscriber
	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.ID, &s.Email, &s.Name); err != nil {
			return nil, err
		}
		subscribers = append(subscribers, s)
	}
	return subscribers, nil
}

func hasReceivedArticle(db *sql.DB, subscriberID, articleID int) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sent_emails WHERE subscriber_id = ? AND article_id = ?",
		subscriberID, articleID).Scan(&count)
	if err != nil {
		log.Printf("Error checking sent email: %v", err)
		return false
	}
	return count > 0
}

func markEmailSent(db *sql.DB, subscriberID, articleID int) {
	_, err := db.Exec("INSERT INTO sent_emails (subscriber_id, article_id) VALUES (?, ?)",
		subscriberID, articleID)
	if err != nil {
		log.Printf("Error marking email as sent: %v", err)
	}
}

func sendEmail(sub Subscriber, article Article) bool {
	// Read the email template file
	templateContent, err := os.ReadFile("email_template.html")
	if err != nil {
		log.Printf("Error reading email template file: %v", err)
		return false
	}

	t, err := template.New("email").Parse(string(templateContent))
	if err != nil {
		log.Printf("Error parsing email template: %v", err)
		return false
	}

	var body bytes.Buffer
	if err := t.Execute(&body, map[string]interface{}{
		"Name":    sub.Name,
		"Title":   article.Title,
		"Content": article.Content,
	}); err != nil {
		log.Printf("Error executing template: %v", err)
		return false
	}

	m := gomail.NewMessage()
	m.SetHeader("From", os.Getenv("EMAIL_FROM"))
	m.SetHeader("To", sub.Email)
	m.SetHeader("Subject", "New Blog Post: "+article.Title)
	m.SetBody("text/html", body.String())

	d := gomail.NewDialer(os.Getenv("SMTP_HOST"), 587, os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD"))

	if err := d.DialAndSend(m); err != nil {
		log.Printf("Error sending email to %s: %v", sub.Email, err)
		return false
	}

	return true
}

func getAllSubscribers(db *sql.DB) ([]Subscriber, error) {
	rows, err := db.Query("SELECT id, email, name, subscribed_at FROM subscribers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subscribers []Subscriber
	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.ID, &s.Email, &s.Name, &s.SubscribedAt); err != nil {
			return nil, err
		}
		subscribers = append(subscribers, s)
	}
	return subscribers, nil
}

func getAllArticles(db *sql.DB) ([]Article, error) {
	rows, err := db.Query("SELECT id, title, content, published_at FROM articles")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.Title, &a.Content, &a.PublishedAt); err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, nil
}

func getAllSentEmails(db *sql.DB) ([]SentEmail, error) {
	rows, err := db.Query("SELECT id, subscriber_id, article_id, sent_at FROM sent_emails")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sentEmails []SentEmail
	for rows.Next() {
		var se SentEmail
		if err := rows.Scan(&se.ID, &se.SubscriberID, &se.ArticleID, &se.SentAt); err != nil {
			return nil, err
		}
		sentEmails = append(sentEmails, se)
	}
	return sentEmails, nil
}

func getAllData(db *sql.DB) (*AllData, error) {
	subscribers, err := getAllSubscribers(db)
	if err != nil {
		return nil, err
	}

	articles, err := getAllArticles(db)
	if err != nil {
		return nil, err
	}

	sentEmails, err := getAllSentEmails(db)
	if err != nil {
		return nil, err
	}

	return &AllData{
		SubscriberCount: len(subscribers),
		SentEmailCount:  len(sentEmails),
		ArticleCount:    len(articles),
		Subscribers:     subscribers,
		SentEmails:      sentEmails,
		Articles:        articles,
	}, nil
}

func handleGetAllData(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := getAllData(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
