package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"pkg.botr.me/yamusic"
)

const (
	port      = 8080
	dbPath    = "settings.db"
	tmplDir   = "web"    // Папка с шаблонами
	staticDir = "static" // Папка со статическими файлами

)

// Инициализация клиента для Яндекс Музыки
var client *yamusic.Client

func openDB() (*sql.DB, error) {
	// Открытие соединения с базой данных
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
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
        track_id INTEGER PRIMARY KEY,
        date_added TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        position INTEGER DEFAULT 0
    );`

	// Создание таблицы settings
	createSettingsQuery := `
    CREATE TABLE IF NOT EXISTS settings (
        id INTEGER PRIMARY KEY,
        user_id INTEGER NOT NULL,
        access_token TEXT NOT NULL
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

	// Подтверждаем транзакцию
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func init() {
	// Создание таблиц, если они не существуют
	err := createTableIfNotExists()
	if err != nil {
		log.Fatalf("Error creating table: %v", err)
	}

	// Открываем соединение с базой данных SQLite
	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Получаем данные из базы данных
	var userID int
	var accessToken string

	row := db.QueryRow("SELECT user_id, access_token FROM settings WHERE id = 2")
	err = row.Scan(&userID, &accessToken)

	// Инициализируем клиента с данными из базы данных
	client = yamusic.NewClient(yamusic.AccessToken(userID, accessToken))
}

// Функция для загрузки шаблона из файла
func loadTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	// Путь к файлу шаблона
	tmplPath := filepath.Join(tmplDir, tmpl)

	// Чтение шаблона
	t, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Printf("Error parsing template: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	// Отображаем шаблон
	err = t.Execute(w, data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Template rendering error", http.StatusInternalServerError)
	}
}

// Страница плейлиста
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
	// Если форма отправлена
	if r.Method == http.MethodPost {
		token := r.FormValue("token")
		userID := r.FormValue("userID")

		// Создаем базу данных и сохраняем данные
		db, err := openDB()
		if err != nil {
			log.Fatal("Failed to open database:", err)
		}
		defer db.Close()

		// Создаем таблицу playlist
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

		// Создаем таблицу settings
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

		// Сохраняем данные
		_, err = db.Exec("INSERT INTO settings (user_id, access_token) VALUES (?, ?)", userID, token)
		if err != nil {
			http.Error(w, "Error saving settings", http.StatusInternalServerError)
			return
		}

		// Перенаправляем на главную страницу
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Показываем форму для ввода данных
	loadTemplate(w, "setup.html", nil)
}
func dbExists() bool {
	_, err := os.Stat(dbPath)
	return !os.IsNotExist(err)
}

// Обработчик для API /api/tracks
func apiTracksHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	db, err := openDB()
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Изменяем запрос для получения track_id и position
	rows, err := db.Query("SELECT track_id, position FROM playlist ORDER BY position")
	if err != nil {
		log.Printf("Error fetching playlist: %v", err)
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tracks []struct {
		TrackID  int    `json:"track_id"`
		Title    string `json:"title"`
		Artist   string `json:"artist"`
		TrackURL string `json:"track_url"`
		CoverURI string `json:"cover_uri"`
		Position int    `json:"position"`
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
			TrackID  int    `json:"track_id"`
			Title    string `json:"title"`
			Artist   string `json:"artist"`
			TrackURL string `json:"track_url"`
			CoverURI string `json:"cover_uri"`
			Position int    `json:"position"`
		}{
			TrackID:  track.TrackID,
			Title:    title,
			Artist:   artist,
			TrackURL: trackURL,
			CoverURI: coverURI,
			Position: track.Position, // Теперь position будет корректно передаваться
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
	if r.Method == http.MethodPost {
		// Чтение данных из тела запроса
		var requestData struct {
			TrackURL string `json:"track_url"` // track_url как строка
		}

		// Декодируем JSON в структуру
		err := json.NewDecoder(r.Body).Decode(&requestData)
		if err != nil {
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

		// Открываем базу данных для добавления трека
		db, err := openDB()
		if err != nil {
			log.Printf("Failed to open database: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		// Проверка, существует ли уже этот трек в плейлисте
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM playlist WHERE track_id = ?)", trackID).Scan(&exists)
		if err != nil {
			log.Printf("Error checking if track exists: %v", err)
			http.Error(w, "Error checking track existence", http.StatusInternalServerError)
			return
		}

		if exists {
			http.Error(w, "Track already exists in the playlist", http.StatusConflict)
			return
		}

		// Вставляем трек в таблицу playlist
		_, err = db.Exec("INSERT INTO playlist (track_id) VALUES (?)", trackID)
		if err != nil {
			log.Printf("Error adding track to playlist: %v", err)
			http.Error(w, "Error adding track to playlist", http.StatusInternalServerError)
			return
		}

		// Перенаправляем на страницу плейлиста после успешного добавления
		http.Redirect(w, r, "/playlist", http.StatusFound)
	} else {
		// Если не POST-запрос, показываем ошибку
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// Функция для изменения позиции трека в плейлисте
func changeTrackPosition(w http.ResponseWriter, r *http.Request) {
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
func extractTrackID(trackURL string) (int, error) {
	// Регулярное выражение для извлечения track_id
	re := regexp.MustCompile(`/track/(\d+)`)
	matches := re.FindStringSubmatch(trackURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("track ID not found in URL")
	}

	// Преобразуем найденный ID в целое число
	var trackID int
	_, err := fmt.Sscanf(matches[1], "%d", &trackID)
	if err != nil {
		return 0, fmt.Errorf("invalid track ID format")
	}

	return trackID, nil
}

// Главная страница
func indexHandler(w http.ResponseWriter, r *http.Request) {
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
		Version:    "0.0.1b",
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
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Устанавливаем заголовок для JSON ответа
	w.Header().Set("Content-Type", "application/json")

	// Декодируем данные запроса
	var requestData struct {
		TrackID int `json:"track_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Открываем базу данных
	db, err := openDB()
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Удаляем трек
	result, err := db.Exec("DELETE FROM playlist WHERE track_id = ?", requestData.TrackID)
	if err != nil {
		log.Printf("Error deleting track from playlist: %v", err)
		http.Error(w, "Error deleting track from playlist", http.StatusInternalServerError)
		return
	}

	// Проверяем, был ли удален трек
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Error checking rows affected: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: rowsAffected > 0,
		Message: "Track successfully deleted",
	}

	// Отправляем JSON ответ
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
}

func main() {
	fs := http.FileServer(http.Dir(staticDir))

	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Проверяем, существует ли база данных
	if !dbExists() {
		http.HandleFunc("/setup", setupHandler)
		log.Println("Database not found, redirecting to setup page...")
	} else {
		// Если база данных существует, загружаем настройки и продолжаем как обычно
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
	}

	log.Printf("Starting server on :%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal(err)
	}
}
