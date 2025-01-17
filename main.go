package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/websocket"
	_ "github.com/gorilla/websocket"
	"golang.org/x/exp/rand"
	_ "modernc.org/sqlite"
	"pkg.botr.me/yamusic"
)

const (
	port      = 8080
	dbPath    = "settings.db"
	tmplDir   = "web"
	staticDir = "static"
)

type Config struct {
	TelegramToken string
	Database      *sql.DB
	YaMusicClient *yamusic.Client
	CoverURI      string
	TrackURL      string
}

var (
	client *yamusic.Client
	db     *sql.DB
	wg     sync.WaitGroup
)

func openDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath) // используем "sqlite" вместо "sqlite3"
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Добавляем проверку соединения
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func createTableIfNotExists() error {
	// Открываем базу данных
	db, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Создание таблицы playlist
	createPlaylistQuery := `
    CREATE TABLE IF NOT EXISTS playlist (
		id integer PRIMARY KEY,
        track_id INTEGER NOT NULL,
        date_added TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		room_id INTEGER DEFAULT 0,
        position INTEGER DEFAULT 0
    );`

	// Создание таблицы settings
	createSettingsQuery := `
    CREATE TABLE IF NOT EXISTS settings (
        id INTEGER PRIMARY KEY,
        user_id INTEGER NOT NULL,
        access_token TEXT NOT NULL
    );`

	createRoomsQuery := `
	CREATE TABLE IF NOT EXISTS rooms (
		id INTEGER PRIMARY KEY,
		code TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Выполняем создание таблиц
	if _, err := tx.Exec(createPlaylistQuery); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create playlist table: %w", err)
	}

	if _, err := tx.Exec(createSettingsQuery); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create settings table: %w", err)
	}

	if _, err := tx.Exec(createRoomsQuery); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create rooms table: %w", err)
	}

	// Подтверждаем транзакцию
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func runTelegramBot(cfg *Config) {
	defer wg.Done()

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		log.Printf("Failed to start Telegram bot: %v", err)
		return
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		switch {
		case update.Message.IsCommand():
			handleCommand(bot, update.Message, cfg)
		case strings.Contains(update.Message.Text, "music.yandex"):
			handleTrackURL(bot, update.Message, cfg)
		}
	}
}

func handleCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message, cfg *Config) {
	var reply string

	switch message.Command() {
	case "start":
		reply = "Привет! Я бот для управления вашим плейлистом. Доступные команды:\n" +
			"/playlist - показать текущий плейлист\n" +
			"/help - показать справку\n" +
			"Также вы можете отправить мне ссылку на трек Яндекс.Музыки для добавления"

	case "help":
		reply = "Доступные команды:\n" +
			"/playlist - показать текущий плейлист\n" +
			"/help - показать эту справку\n\n" +
			"/next - переключиться на следующий трек\n" +
			"/prev - переключиться на предыдущий трек\n" +
			"/now - показать текущий трек\n" +
			"/pause - поставить текущий трек на паузу\n\n" +
			"Для добавления трека отправьте ссылку на него с Яндекс.Музыки\n" +
			"Для удаления трека используйте кнопку удаления в списке плейлиста"

	case "next":
		// отправляем wsBroadcast сообщение
		wsBroadcast <- map[string]string{
			"type": "next",
		}
		reply = "Переключение на следующий трек"

	case "now":
		// отправляем wsBroadcast сообщение
		wsBroadcast <- map[string]string{
			"type": "now",
		}
		reply = "Показать текущий трек"

	case "prev":
		// отправляем wsBroadcast сообщение
		wsBroadcast <- map[string]string{
			"type": "prev",
		}
		reply = "Переключение на предыдущий трек"

	case "pause":
		// отправляем wsBroadcast сообщение
		wsBroadcast <- map[string]string{
			"type": "pause",
		}
		reply = "Пауза"

	case "playlist":
		tracks, err := getPlaylist(context.Background(), cfg)
		if err != nil {
			reply = "Ошибка при получении плейлиста: " + err.Error()
		} else if len(tracks) == 0 {
			reply = "Плейлист пуст"
		} else {
			var sb strings.Builder
			sb.WriteString("Ваш плейлист:\n\n")
			for i, track := range tracks {
				sb.WriteString(fmt.Sprintf("%d. %s - %s\n", i+1, track.Artist, track.Title))
			}
			reply = sb.String()
		}

	case "notify":
		wsBroadcast <- map[string]string{
			"type":    "notification",
			"message": "Новая команда от Telegram-бота: " + message.Text,
		}
		reply = "Уведомление отправлено на фронтенд"

	default:
		reply = "Неизвестная команда. Используйте /help для просмотра доступных команд"
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, reply)
	bot.Send(msg)
}

type Track struct {
	TrackID int
	Title   string
	Artist  string
}

func getPlaylist(ctx context.Context, cfg *Config) ([]Track, error) {
	rows, err := cfg.Database.QueryContext(ctx, "SELECT track_id FROM playlist ORDER BY position")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var trackID int
		if err := rows.Scan(&trackID); err != nil {
			return nil, err
		}

		trackInfo, resp, err := cfg.YaMusicClient.Tracks().Get(ctx, trackID)
		if err != nil || resp.StatusCode != 200 {
			continue
		}

		tracks = append(tracks, Track{
			TrackID: trackID,
			Title:   trackInfo.Result[0].Title,
			Artist:  trackInfo.Result[0].Artists[0].Name,
		})
	}

	return tracks, rows.Err()
}

func checkTrackExists(trackID int, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM playlist WHERE track_id = ?)", trackID).Scan(&exists)
	return exists, err
}

func addTrackToPlaylist(trackID int, db *sql.DB) error {
	_, err := db.Exec("INSERT INTO playlist (track_id) VALUES (?)", trackID)
	return err
}

func loadTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	tmplPath := filepath.Join(tmplDir, tmpl)

	t, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Printf("Error parsing template: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Template rendering error", http.StatusInternalServerError)
	}
}

func playlistHandler(w http.ResponseWriter, r *http.Request) {
	// Получаем все треки из базы данных
	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Запрашиваем все треки по их track_id
	rows, err := db.Query("SELECT track_id FROM playlist")
	if err != nil {
		log.Printf("Error fetching playlist: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tracks []struct {
		TrackID  int
		Title    string
		Artist   string
		TrackURL string
		CoverURI string
	}

	// Проходим по каждому треку
	for rows.Next() {
		var track struct {
			TrackID int
		}
		if err := rows.Scan(&track.TrackID); err != nil {
			log.Printf("Error scanning row: %v", err)
			http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
			return
		}

		// Получаем информацию о треке с API Яндекс Музыки
		trackInfo, resp, err := client.Tracks().Get(r.Context(), track.TrackID)
		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("Error getting track info: %v", err)
			continue // Если не удалось получить информацию, пропускаем этот трек
		}

		// Получаем URL для скачивания
		trackURL, err := client.Tracks().GetDownloadURL(r.Context(), track.TrackID)
		if err != nil {
			log.Printf("Error getting track download URL: %v", err)
			continue
		}

		// Исправляем URL для обложки
		coverURI := trackInfo.Result[0].Albums[0].CoverURI
		coverURI = strings.Replace(coverURI, "%25%25", "400x400", -1)
		coverURI = strings.Replace(coverURI, "%", "", -1)

		// Добавляем всю информацию в список
		tracks = append(tracks, struct {
			TrackID  int
			Title    string
			Artist   string
			TrackURL string
			CoverURI string
		}{
			TrackID:  track.TrackID,
			Title:    trackInfo.Result[0].Title,
			Artist:   trackInfo.Result[0].Artists[0].Name,
			TrackURL: trackURL,
			CoverURI: coverURI,
		})
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating rows: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}

	// Передаем данные о плейлисте в шаблон
	loadTemplate(w, "playlist.html", tracks)
}

func setupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		token := r.FormValue("token")
		userID := r.FormValue("userID")

		db, err := openDB()
		if err != nil {
			log.Fatal("Failed to open database:", err)
		}
		defer db.Close()

		_, err = db.Exec(`
            CREATE TABLE IF NOT EXISTS playlist (
                track_id INTEGER PRIMARY KEY,
                date_added TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                position INTEGER DEFAULT 0
            );`)
		if err != nil {
			http.Error(w, "Error creating playlist table", http.StatusInternalServerError)
			return
		}

		_, err = db.Exec(`
            CREATE TABLE IF NOT EXISTS settings (
                id INTEGER PRIMARY KEY,
                user_id INTEGER NOT NULL,
                access_token TEXT NOT NULL
            );`)
		if err != nil {
			http.Error(w, "Error creating settings table", http.StatusInternalServerError)
			return
		}

		_, err = db.Exec("INSERT INTO settings (user_id, access_token) VALUES (?, ?)", userID, token)
		if err != nil {
			http.Error(w, "Error saving settings", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	loadTemplate(w, "setup.html", nil)
}
func dbExists() bool {
	_, err := os.Stat(dbPath)
	return !os.IsNotExist(err)
}

// Обработчик для API /api/tracks
func apiTracksHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// требуем room_code
	roomCode := r.URL.Query().Get("room_code")
	if roomCode == "" {
		http.Error(w, "Room code is required", http.StatusBadRequest)
		return
	}

	// проверяем is_room_exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM rooms WHERE code = ?)", roomCode).Scan(&exists)
	if err != nil {
		log.Printf("Error checking room existence: %v", err)
		http.Error(w, "Error checking room existence", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Изменяем запрос для получения track_id и position
	rows, err := db.Query("SELECT track_id, position FROM playlist WHERE room_id = ? ORDER BY id", roomCode)
	if err != nil {
		log.Printf("Error fetching playlist: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tracks []struct {
		TrackID    int    `json:"track_id"`
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		TrackURL   string `json:"track_url"`
		CoverURI   string `json:"cover_uri"`
		Position   int    `json:"position"`
		DurationMs int    `json:"duration_ms"`
	}

	for rows.Next() {
		var track struct {
			TrackID  int
			Position int
		}
		// Сканируем оба поля
		if err := rows.Scan(&track.TrackID, &track.Position); err != nil {
			log.Printf("Error scanning row: %v", err)
			http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
			return
		}

		trackInfo, resp, err := client.Tracks().Get(r.Context(), track.TrackID)
		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("Error getting track info: %v", err)
			continue
		}

		title := trackInfo.Result[0].Title
		artist := trackInfo.Result[0].Artists[0].Name

		trackURL, err := client.Tracks().GetDownloadURL(r.Context(), track.TrackID)
		if err != nil {
			log.Printf("Error getting track download URL: %v", err)
			continue
		}

		coverURI := trackInfo.Result[0].Albums[0].CoverURI
		coverURI = strings.Replace(coverURI, "%25%25", "400x400", -1)
		coverURI = strings.Replace(coverURI, "%", "", -1)

		tracks = append(tracks, struct {
			TrackID    int    `json:"track_id"`
			Title      string `json:"title"`
			Artist     string `json:"artist"`
			TrackURL   string `json:"track_url"`
			CoverURI   string `json:"cover_uri"`
			Position   int    `json:"position"`
			DurationMs int    `json:"duration_ms"`
		}{
			TrackID:    track.TrackID,
			Title:      title,
			Artist:     artist,
			TrackURL:   trackURL,
			CoverURI:   coverURI,
			Position:   track.Position, // Теперь position будет корректно передаваться
			DurationMs: trackInfo.Result[0].DurationMs,
		})
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating rows: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(tracks)
	if err != nil {
		log.Printf("Error encoding tracks to JSON: %v", err)
		http.Error(w, "Error encoding tracks to JSON", http.StatusInternalServerError)
	}
}

// Обработчик для добавления трека в плейлист
func addTrackToPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Чтение данных из тела запроса
	var requestData struct {
		TrackURL string `json:"track_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		log.Printf("Error decoding JSON: %v", err)
		http.Error(w, "Invalid request data", http.StatusBadRequest)
		return
	}

	// Извлекаем track_id из URL
	trackID, err := extractTrackID(requestData.TrackURL)
	if err != nil {
		log.Printf("Error extracting track ID: %v", err)
		http.Error(w, "Invalid track URL", http.StatusBadRequest)
		return
	}

	// Проверка существования трека в базе данных
	exists, err := checkTrackExists(trackID, db)
	if err != nil {
		log.Printf("Error checking track existence: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if exists {
		http.Error(w, "Track already exists in the playlist", http.StatusConflict)
		return
	}

	// Добавление трека в плейлист
	err = addTrackToPlaylist(trackID, db)
	if err != nil {
		log.Printf("Error adding track to playlist: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Отправляем успешный ответ
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte("Track added successfully"))
}

// Функция для изменения позиции трека в плейлисте
func changeTrackPosition(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Проверяем, что метод запроса - POST
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	// Чтение данных из тела запроса
	var requestData struct {
		TrackID  int `json:"track_id"`
		Position int `json:"position"`
	}
	// Декодируем JSON в структуру
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		log.Printf("Error decoding JSON: %v", err)
		http.Error(w, "Invalid request data", http.StatusBadRequest)
		return
	}
	// Открываем базу данных для изменения позиции трека
	db, err := openDB()
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer db.Close()
	// Обновляем позицию трека в плейлисте
	_, err = db.Exec("UPDATE playlist SET position = ? WHERE track_id = ?", requestData.Position, requestData.TrackID)
	if err != nil {
		log.Printf("Error updating track position: %v", err)
		http.Error(w, "Error updating track position", http.StatusInternalServerError)
		return
	}
	// Отправляем ответ об успешном изменении позиции
	w.WriteHeader(http.StatusOK)
}

// Функция для извлечения track_id из URL
func extractTrackID(input string) (int, error) {
	// Сначала проверяем, не является ли input просто числом
	if trackID, err := strconv.Atoi(strings.TrimSpace(input)); err == nil {
		return trackID, nil
	}

	// Если не число, ищем ID в URL
	re := regexp.MustCompile(`/track/(\d+)$|/album/\d+/track/(\d+)`)
	matches := re.FindStringSubmatch(input)

	if len(matches) < 2 {
		return 0, fmt.Errorf("track ID not found in input: %s", input)
	}

	// Ищем непустую группу (первая или вторая)
	var trackIDStr string
	if matches[1] != "" {
		trackIDStr = matches[1]
	} else if matches[2] != "" {
		trackIDStr = matches[2]
	}

	// Преобразуем ID в число
	trackID, err := strconv.Atoi(trackIDStr)
	if err != nil {
		return 0, fmt.Errorf("invalid track ID format")
	}

	return trackID, nil
}

// Обновляем handleTrackURL для корректной обработки
func handleTrackURL(bot *tgbotapi.BotAPI, message *tgbotapi.Message, cfg *Config) {
	// Пытаемся извлечь ID из сообщения
	trackID, err := extractTrackID(message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"Неверный формат. Отправьте ссылку на трек или его ID")
		bot.Send(msg)
		return
	}

	// Проверяем существование трека
	exists, err := checkTrackExists(trackID, cfg.Database)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при проверке трека")
		bot.Send(msg)
		return
	}
	if exists {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Этот трек уже есть в плейлисте")
		bot.Send(msg)
		return
	}

	// Получаем информацию о треке
	trackInfo, resp, err := cfg.YaMusicClient.Tracks().Get(context.Background(), trackID)
	if err != nil || resp.StatusCode != 200 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении информации о треке")
		bot.Send(msg)
		return
	}

	// Добавляем трек в базу
	err = addTrackToPlaylist(trackID, cfg.Database)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при добавлении трека")
		bot.Send(msg)
		return
	}

	reply := fmt.Sprintf("Трек добавлен в плейлист:\n%s - %s",
		trackInfo.Result[0].Artists[0].Name,
		trackInfo.Result[0].Title)
	msg := tgbotapi.NewMessage(message.Chat.ID, reply)
	bot.Send(msg)
}

// Главная страница
func indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Получаем информацию о пользователе с помощью API Яндекс Музыки
	accountStatus, resp, err := client.Account().GetStatus(r.Context())
	if err != nil {
		log.Printf("Error getting account status: %v", err)
		http.Error(w, "Error fetching account status", http.StatusInternalServerError)
		return
	}
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Error fetching account status", http.StatusInternalServerError)
		return
	}

	// Передаем информацию о пользователе в шаблон
	data := struct {
		UserID   int
		FullName string
	}{
		UserID:   accountStatus.Result.Account.UID,
		FullName: accountStatus.Result.Account.FullName,
	}

	// Отображаем шаблон с данными
	loadTemplate(w, "index.html", data)
}

func settingsTemplate(w http.ResponseWriter, r *http.Request) {
	loadTemplate(w, "settings.html", nil)
}

// Получение информации о треке
func getTrackHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	trackID := r.URL.Query().Get("trackID")
	if trackID == "" {
		http.Error(w, "Track ID is required", http.StatusBadRequest)
		return
	}

	// Получение информации о треке с использованием API
	trackIDInt, err := strconv.Atoi(trackID)
	if err != nil {
		http.Error(w, "Invalid track ID", http.StatusBadRequest)
		return
	}

	trackInfo, resp, err := client.Tracks().Get(r.Context(), trackIDInt)
	if err != nil {
		log.Printf("Error getting track info: %v", err)
		http.Error(w, "Error getting track info", http.StatusInternalServerError)
		return
	}
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Error fetching track data", http.StatusInternalServerError)
		return
	}

	// Получаем URL для скачивания
	trackURL, err := client.Tracks().GetDownloadURL(r.Context(), trackIDInt)
	if err != nil {
		log.Printf("Error getting track download URL: %v", err)
		http.Error(w, "Error fetching track download URL", http.StatusInternalServerError)
		return
	}

	// Исправляем URL для обложки
	coverURI := trackInfo.Result[0].Albums[0].CoverURI
	coverURI = strings.Replace(coverURI, "%25%25", "400x400", -1)
	coverURI = strings.Replace(coverURI, "%", "", -1) // Убираем символы %

	// Передаем данные о треке и URL для воспроизведения
	data := struct {
		TrackInfo yamusic.Track
		TrackURL  string
		CoverURI  string
	}{
		TrackInfo: trackInfo.Result[0], // Выбираем первый результат
		TrackURL:  trackURL,            // URL для воспроизведения
		CoverURI:  coverURI,            // Исправленный URL обложки
	}

	// Отображаем страницу с информацией о треке и плеером
	loadTemplate(w, "trackInfo.html", data)
}

// Сохранение настроек API
func saveSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		token := r.FormValue("token")
		userID := r.FormValue("userID")

		// Сохранение данных в базе данных
		db, err := openDB()
		if err != nil {
			log.Fatal("Failed to open database:", err)
		}
		defer db.Close()

		_, err = db.Exec("UPDATE settings SET user_id = ?, access_token = ? WHERE id = 2", userID, token)
		if err != nil {
			log.Printf("Error saving settings: %v", err)
			http.Error(w, "Error saving settings", http.StatusInternalServerError)
			return
		}

		// Передаем флаг успешности
		data := struct {
			Success bool
		}{
			Success: true,
		}

		// Отображаем страницу настроек с сообщением об успешном сохранении
		loadTemplate(w, "settings.html", data)
	} else {
		// Для GET-запросов просто показываем форму настроек
		loadTemplate(w, "settings.html", nil)
	}
}

type AppInfo struct {
	Name       string
	Version    string
	BuildTime  string
	CommitHash string
	StartTime  time.Time
	Uptime     time.Duration
}

func debugHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Информация о приложении
	appInfo := AppInfo{
		Name:       "MusicDirect",
		Version:    "0.0.4",
		BuildTime:  os.Getenv("BUILD_TIME"),
		CommitHash: os.Getenv("COMMIT_HASH"),
		StartTime:  startTime,
		Uptime:     time.Since(startTime),
	}

	// Расширенная структура для отладочной информации
	data := struct {
		App          AppInfo
		GoVersion    string
		OS           string
		Arch         string
		NumCPU       int
		NumGoroutine int
		GOPATH       string
		GOROOT       string
		MemStats     runtime.MemStats
		Time         time.Time
		Environment  map[string]string
		DiskUsage    struct {
			Total uint64
			Free  uint64
			Used  uint64
		}
		Network struct {
			Interfaces  []string
			Connections int
		}
		Database struct {
			Connected bool
			Stats     sql.DBStats
		}
	}{
		App:          appInfo,
		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		GOPATH:       os.Getenv("GOPATH"),
		GOROOT:       runtime.GOROOT(),
		Time:         time.Now(),
		Environment:  make(map[string]string),
	}

	// Получаем статистику БД
	db, err := openDB()
	if err == nil {
		data.Database.Connected = true
		data.Database.Stats = db.Stats()
		defer db.Close()
	} else {
		data.Database.Connected = false
	}

	// Данные аккаунта Яндекс Музыки
	accountStatus, _, err := client.Account().GetStatus(r.Context())
	if err == nil {
		data.Environment["YandexMusicUserID"] = strconv.Itoa(accountStatus.Result.Account.UID)
		data.Environment["YandexMusicDisplayName"] = accountStatus.Result.Account.DisplayName
		data.Environment["YandexMusicLogin"] = accountStatus.Result.Account.Login
	}

	// Получаем статистику памяти
	runtime.ReadMemStats(&data.MemStats)

	// шаблон
	loadTemplate(w, "debug.html", data)

}

func deleteTrackFromPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Log the raw request body
	body, _ := io.ReadAll(r.Body)
	log.Printf("Received delete request body: %s", string(body))
	r.Body = io.NopCloser(bytes.NewBuffer(body)) // Restore the body for later use

	var requestData struct {
		TrackID  int    `json:"track_id"`
		RoomCode string `json:"room_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		log.Printf("Error decoding request: %v", err)
		http.Error(w, "Invalid request data", http.StatusBadRequest)
		return
	}

	log.Printf("Decoded request data: track_id=%d, room_code=%s", requestData.TrackID, requestData.RoomCode)

	db, err := openDB()
	if err != nil {
		log.Printf("Database connection error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// First, check if the room exists
	var roomID int
	err = db.QueryRow("SELECT id FROM rooms WHERE code = ?", requestData.RoomCode).Scan(&roomID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Room not found with code: %s", requestData.RoomCode)
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
		log.Printf("Error querying room: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	log.Printf("Found room ID: %d for code: %s", roomID, requestData.RoomCode)

	// Check if the track exists in the playlist
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM playlist WHERE track_id = ? AND room_id = ?)",
		requestData.TrackID, roomID).Scan(&exists)
	if err != nil {
		log.Printf("Error checking track existence: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !exists {
		log.Printf("Track %d not found in room %d", requestData.TrackID, roomID)
		http.Error(w, "Track not found in playlist", http.StatusNotFound)
		return
	}

	// Delete the track
	result, err := db.Exec("DELETE FROM playlist WHERE track_id = ? AND room_id = ?",
		requestData.TrackID, roomID)
	if err != nil {
		log.Printf("Delete error detail: %v", err)
		http.Error(w, fmt.Sprintf("Delete error: %v", err), http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Error getting rows affected: %v", err)
	} else {
		log.Printf("Delete operation affected %d rows", rowsAffected)
	}

	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: rowsAffected > 0,
		Message: fmt.Sprintf("Successfully deleted track from playlist"),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// Выводим все track_id из базы данных
func getDBTracksIDHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// принимаем room_code из запроса
	roomCode := r.URL.Query().Get("room_code")
	// если room_code не передан, возвращаем ошибку
	if roomCode == "" {
		http.Error(w, "Room code is required", http.StatusBadRequest)
		return
	}

	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM rooms WHERE code = ?)", roomCode).Scan(&exists)
	if err != nil {
		log.Printf("Error checking room existence: %v", err)
		http.Error(w, "Error checking room existence", http.StatusInternalServerError)
		return
	}

	// Открываем базу данных
	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Запрашиваем все track_id
	rows, err := db.Query("SELECT track_id FROM playlist WHERE room_id = ?", roomCode)
	if err != nil {
		log.Printf("Error fetching playlist: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Список track_id
	var trackIDs []int

	// Проходим по каждому track_id
	for rows.Next() {
		var trackID int
		if err := rows.Scan(&trackID); err != nil {
			log.Printf("Error scanning row: %v", err)
			http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
			return
		}

		// Добавляем track_id в список
		trackIDs = append(trackIDs, trackID)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating rows: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}

	// Отправляем список track_id в формате JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(trackIDs); err != nil {
		log.Printf("Error encoding track IDs: %v", err)
		http.Error(w, "Error encoding track IDs", http.StatusInternalServerError)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Настройте политику CORS, если необходимо
	},
}

var wsClients = make(map[*websocket.Conn]bool) // Хранение активных соединений
var wsBroadcast = make(chan interface{})       // Канал для отправки сообщений клиентам

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()
	wsClients[conn] = true

	for {
		var msg interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("WebSocket read error: %v", err)
			delete(wsClients, conn)
			break
		}
	}
}

func wsBroadcastMessages() {
	for {
		msg := <-wsBroadcast
		for client := range wsClients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.Close()
				delete(wsClients, client)
			}
		}
	}
}

type TrackInfo struct {
	TrackID  int    `json:"track_id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	TrackURL string `json:"track_url"`
	CoverURI string `json:"cover_uri"`
	Position int    `json:"position"`
}

// getTrackInfo retrieves complete track information from Yandex Music
func getTrackInfo(ctx context.Context, trackID int, client *yamusic.Client) (*TrackInfo, error) {
	if client == nil {
		return nil, fmt.Errorf("yandex music client is not initialized")
	}

	// Get track information
	trackInfo, resp, err := client.Tracks().Get(ctx, trackID)
	if err != nil {
		return nil, fmt.Errorf("failed to get track info: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yandex music API returned non-200 status: %d", resp.StatusCode)
	}
	if len(trackInfo.Result) == 0 {
		return nil, fmt.Errorf("no track information found for ID: %d", trackID)
	}

	// Get download URL
	trackURL, err := client.Tracks().GetDownloadURL(ctx, trackID)
	if err != nil {
		return nil, fmt.Errorf("failed to get track download URL: %w", err)
	}

	// Process cover URI
	coverURI := trackInfo.Result[0].Albums[0].CoverURI
	coverURI = strings.Replace(coverURI, "%25%25", "400x400", -1)
	coverURI = strings.Replace(coverURI, "%", "", -1)

	// Get track position from database if needed
	var position int
	if db != nil { // Assuming db is a global variable
		err := db.QueryRowContext(ctx,
			"SELECT position FROM playlist WHERE track_id = ?",
			trackID).Scan(&position)
		if err != nil && err != sql.ErrNoRows {
			log.Printf("Warning: failed to get track position: %v", err)
			// Don't return error as position is non-critical
		}
	}

	return &TrackInfo{
		TrackID:  trackID,
		Title:    trackInfo.Result[0].Title,
		Artist:   trackInfo.Result[0].Artists[0].Name,
		TrackURL: trackURL,
		CoverURI: coverURI,
		Position: position,
	}, nil
}

// getTrackInfoHandler handles HTTP requests for track information
func getTrackInfoHandler(w http.ResponseWriter, r *http.Request) {
	// CORS разрешить использование для всех запросов
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Set response headers
	w.Header().Set("Content-Type", "application/json")

	// Get and validate track ID from query parameters
	trackIDStr := r.URL.Query().Get("trackID")
	if trackIDStr == "" {
		http.Error(w, `{"error":"trackID is required"}`, http.StatusBadRequest)
		return
	}

	trackID, err := strconv.Atoi(trackIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid trackID format"}`, http.StatusBadRequest)
		return
	}

	// Get track information with context
	trackInfo, err := getTrackInfo(r.Context(), trackID, client)
	if err != nil {
		log.Printf("Error getting track info: %v", err)
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			statusCode = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), statusCode)
		return
	}

	// Encode and send response
	if err := json.NewEncoder(w).Encode(trackInfo); err != nil {
		log.Printf("Error encoding track info response: %v", err)
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}

func createRoomHandler(w http.ResponseWriter, r *http.Request) {
	db, err := openDB()
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Генерируем уникальный код комнаты (6 символов)
	code := generateRoomCode(5)

	// Создаем комнату в БД
	result, err := db.Exec(`
        INSERT INTO rooms (code, created_at) 
        VALUES (?, CURRENT_TIMESTAMP)`,
		code,
	)
	if err != nil {
		log.Printf("Error creating room: %v", err)
		http.Error(w, "Error creating room", http.StatusInternalServerError)
		return
	}

	// Получаем ID созданной комнаты
	roomID, _ := result.LastInsertId()

	// Отправляем ответ с кодом комнаты
	response := struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}{
		ID:   roomID,
		Code: code,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SQL для создания таблицы rooms
func createRoomsTable(db *sql.DB) error {
	_, err := db.Exec(`
    CREATE TABLE IF NOT EXISTS rooms (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        code TEXT NOT NULL UNIQUE,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )`)
	return err
}

func joinRoomHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Чтение данных из тела запроса
	var requestData struct {
		RoomCode string `json:"room_code"`
	}

	// Декодируем JSON в структуру
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		log.Printf("Error decoding JSON: %v", err)
		http.Error(w, "Invalid request data", http.StatusBadRequest)
		return
	}

	// Проверяем, существует ли комната с таким кодом
	var roomID int
	err = db.QueryRow("SELECT id FROM rooms WHERE code = ?", requestData.RoomCode).Scan(&roomID)
	if err != nil {
		log.Printf("Error checking room existence: %v", err)
		http.Error(w, "Error checking room existence", http.StatusInternalServerError)
		return
	}

	// Отправляем ответ с ID комнаты
	response := struct {
		RoomID int `json:"room_id"`
	}{
		RoomID: roomID,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
	}
}

func generateRoomCode(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	code := make([]byte, length)
	for i := range code {
		code[i] = charset[rand.Intn(len(charset))]
	}
	return string(code)
}

func getRoomPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	roomIDStr := r.URL.Query().Get("roomID")
	if roomIDStr == "" {
		http.Error(w, "Room ID is required", http.StatusBadRequest)
		return
	}

	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		http.Error(w, "Invalid room ID", http.StatusBadRequest)
		return
	}

	// Получаем треки комнаты
	tracks, err := getRoomTracks(roomID)
	if err != nil {
		log.Printf("Error getting room tracks: %v", err)
		http.Error(w, "Error getting room tracks", http.StatusInternalServerError)
		return
	}

	// Отправляем треки в формате JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tracks); err != nil {
		log.Printf("Error encoding room tracks: %v", err)
		http.Error(w, "Error encoding room tracks", http.StatusInternalServerError)
	}
}

func getRoomTracks(roomID int) ([]TrackInfo, error) {
	rows, err := db.Query("SELECT track_id FROM playlist WHERE room_id = ? ORDER BY position", roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []TrackInfo
	for rows.Next() {
		var trackID int
		if err := rows.Scan(&trackID); err != nil {
			return nil, err
		}

		trackInfo, err := getTrackInfo(context.Background(), trackID, client)
		if err != nil {
			log.Printf("Error getting track info: %v", err)
			continue
		}

		tracks = append(tracks, *trackInfo)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tracks, nil
}

func isExistRoomCode(code string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM rooms WHERE code = ?)", code).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

//

/*
    пример js кода для получения информации о треке
	fetch("/api/track?trackID=12345")
*/

func main() {
	// Initialize database connection
	var err error
	db, err = openDB()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close() // Close database when main exits

	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/ws", wsHandler)
	go wsBroadcastMessages()

	// Разрешить использование cors для всех запросов

	if !dbExists() {
		http.HandleFunc("/setup", setupHandler)
		log.Println("Database not found, redirecting to setup page...")
	} else {
		http.HandleFunc("/", indexHandler)
		http.HandleFunc("/debug", debugHandler)
		http.HandleFunc("/settings", saveSettingsHandler)
		http.HandleFunc("/page/settings", settingsTemplate)
		http.HandleFunc("/get-track", getTrackHandler)
		http.HandleFunc("/playlist", playlistHandler)
		http.HandleFunc("/add-track", addTrackToPlaylistHandler)
		http.HandleFunc("/api/tracks", apiTracksHandler)
		http.HandleFunc("/api/tracks/changeposition", changeTrackPosition)
		http.HandleFunc("/api/tracks/delete", deleteTrackFromPlaylistHandler)
		http.HandleFunc("/api/tracks/all", getDBTracksIDHandler)
		http.HandleFunc("/api/track", getTrackInfoHandler)
		http.HandleFunc("/api/room/create", createRoomHandler)
		http.HandleFunc("/api/room/join", joinRoomHandler)
		http.HandleFunc("/api/room/status", getRoomPlaylistHandler)
	}

	log.Printf("Starting server on :%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal(err)
	}
}

func init() {
	err := createTableIfNotExists()
	if err != nil {
		log.Fatalf("Error creating table: %v", err)
	}

	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	var userID int
	var accessToken string

	row := db.QueryRow("SELECT user_id, access_token FROM settings WHERE id = 2")
	err = row.Scan(&userID, &accessToken)
	if err != nil {
		log.Printf("Warning: Could not load Yandex Music settings: %v", err)
	}

	client = yamusic.NewClient(yamusic.AccessToken(userID, accessToken))

	db.Close()
}
