package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	_ "modernc.org/sqlite"
)

// Конфигурационная структура
type Config struct {
	Feeds  []string `json:"feeds"`
	Period int      `json:"period"`
}

// Структура для RSS
type RSS struct {
	Channel struct {
		Items []Item `xml:"item"`
	} `xml:"channel"`
}

// Структура для Item
type Item struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
}

var db *sql.DB

// Инициализация базы данных
func initDB() {
	var err error
	db, err = sql.Open("sqlite", "./rss.db")
	if err != nil {
		log.Fatal(err)
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS rss (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,		
		"title" TEXT,
		"description" TEXT,
		"link" TEXT,
		"pubDate" DATETIME
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatal(err)
	}
}

// Функция для добавления публикации в базу данных
func insertItem(item Item) {
	insertSQL := `INSERT INTO rss (title, description, link, pubDate) VALUES (?, ?, ?, ?)`
	_, err := db.Exec(insertSQL, item.Title, item.Description, item.Link, item.PubDate)
	if err != nil {
		log.Printf("Error inserting item: %v", err)
	}
}

// Обработка RSS
func fetchRSS(url string, wg *sync.WaitGroup) {
	defer wg.Done()

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error fetching URL %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	var rss RSS
	err = xml.Unmarshal(body, &rss)
	if err != nil {
		log.Printf("Error unmarshalling XML: %v", err)
		return
	}

	for _, item := range rss.Channel.Items {
		insertItem(item)
	}
}

// Чтение конфигурационного файла
func readConfig(filename string) (Config, error) {
	var config Config
	configFile, err := os.Open(filename)
	if err != nil {
		return config, err
	}
	defer configFile.Close()

	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(&config)
	return config, err
}

// Периодическая проверка RSS
func pollFeeds(config Config) {
	ticker := time.NewTicker(time.Duration(config.Period) * time.Minute)
	for range ticker.C {
		var wg sync.WaitGroup
		for _, url := range config.Feeds {
			wg.Add(1)
			go fetchRSS(url, &wg)
		}
		wg.Wait()
	}
}

// API для получения публикаций
func apiHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	count, err := strconv.Atoi(vars["count"])
	if err != nil {
		http.Error(w, "Invalid count parameter", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(`SELECT title, description, link, pubDate FROM rss ORDER BY pubDate DESC LIMIT ?`, count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		var item Item
		err := rows.Scan(&item.Title, &item.Description, &item.Link, &item.PubDate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func main() {
	// Чтение конфигурационного файла
	config, err := readConfig("config.json")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	// Инициализация базы данных
	initDB()
	defer db.Close()

	// Запуск периодического обхода RSS-лент
	go pollFeeds(config)

	// Настройка маршрутов HTTP
	r := mux.NewRouter()
	r.HandleFunc("/api/news/{count}", apiHandler).Methods("GET")

	// Настройка статических файлов
	fs := http.FileServer(http.Dir("./static"))
	r.PathPrefix("/").Handler(fs)

	// Запуск сервера
	log.Fatal(http.ListenAndServe(":8080", r))
}
