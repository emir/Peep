package main

import (
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "modernc.org/sqlite"
)

// EmailSender yapısı
type EmailSender struct {
	FullName string
	Email    string
}

// Progress yapısı - ilerleme takibi için
type Progress struct {
	LastProcessedUID uint32
	TotalMessages    uint32
	ProcessedCount   uint32
	StartTime        time.Time
}

// Config yapısı
type Config struct {
	IMAPServer   string
	Username     string
	Password     string
	DBPath       string
	LogPath      string
	BatchSize    int
	ShowProgress bool
	ShowHelp     bool
	Verbose      bool
}

// Komut satırı parametrelerini parse et
func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.IMAPServer, "server", "imap.gmail.com:993", "IMAP sunucu adresi")
	flag.StringVar(&config.Username, "user", "", "E-posta kullanıcı adı (zorunlu)")
	flag.StringVar(&config.Password, "pass", "", "E-posta şifresi (zorunlu)")
	flag.StringVar(&config.DBPath, "db", "", "Veritabanı dosya yolu (otomatik)")
	flag.StringVar(&config.LogPath, "log", "", "Log dosya yolu (otomatik)")
	flag.IntVar(&config.BatchSize, "batch", 500, "Batch boyutu (100-2000)")
	flag.BoolVar(&config.ShowProgress, "progress", true, "İlerleme durumunu göster")
	flag.BoolVar(&config.Verbose, "verbose", false, "Detaylı log çıktısı")
	flag.BoolVar(&config.ShowHelp, "help", false, "Yardım göster")

	flag.Parse()

	if config.ShowHelp {
		showUsage()
		os.Exit(0)
	}

	if config.Username == "" || config.Password == "" {
		fmt.Println("❌ Hata: -user ve -pass parametreleri zorunludur!")
		showUsage()
		os.Exit(1)
	}

	// Güvenli dosya adı oluştur
	safeUsername := strings.ReplaceAll(config.Username, "@", "_at_")
	safeUsername = strings.ReplaceAll(safeUsername, ".", "_")

	// Klasörleri oluştur
	os.MkdirAll("./db", 0755)
	os.MkdirAll("./logs", 0755)

	if config.DBPath == "" {
		config.DBPath = filepath.Join("./db", safeUsername+".db")
	}

	if config.LogPath == "" {
		timestamp := time.Now().Format("2006-01-02")
		config.LogPath = filepath.Join("./logs", safeUsername+"_"+timestamp+".log")
	}

	if config.BatchSize < 100 || config.BatchSize > 2000 {
		config.BatchSize = 500
	}

	return config
}

// Kullanım bilgisi göster
func showUsage() {
	fmt.Println(`
📧 E-POSTA GÖNDEREN TARAYICI

KULLANIM:
  go run main.go -user <email> -pass <password> [seçenekler]

ZORUNLU PARAMETRELER:
  -user <email>     E-posta adresi
  -pass <password>  E-posta şifresi (Gmail için uygulama şifresi)

SEÇENEKLER:
  -server <server>  IMAP sunucu adresi (varsayılan: imap.gmail.com:993)
  -db <path>        Veritabanı dosya yolu (otomatik: ./db/{username}.db)
  -log <path>       Log dosya yolu (otomatik: ./logs/{username}_{date}.log)
  -batch <size>     Batch boyutu 100-2000 (varsayılan: 500)
  -progress <bool>  İlerleme göster (varsayılan: true)
  -verbose          Detaylı log çıktısı
  -help             Bu yardımı göster

ÖRNEKLER:
  go run main.go -user john@gmail.com -pass abcdefghijklmnop
  go run main.go -user john@outlook.com -pass mypass -server outlook.office365.com:993
  go run main.go -user john@gmail.com -pass mypass -batch 100 -verbose
`)
}

// Log sistemi kurulumu
func setupLogging(config *Config) {
	logFile, err := os.OpenFile(config.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("❌ Log dosyası oluşturulamadı: %v\n", err)
		os.Exit(1)
	}

	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Başlangıç logu
	log.Printf("=== YENİ TARAMA BAŞLADI ===")
	log.Printf("Kullanıcı: %s", config.Username)
	log.Printf("Sunucu: %s", config.IMAPServer)
	log.Printf("Veritabanı: %s", config.DBPath)
	log.Printf("Batch boyutu: %d", config.BatchSize)
}

// Veritabanı yapılandırması
func initDB(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("klasör oluşturulamadı: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Senders tablosu
	createSendersTable := `
	CREATE TABLE IF NOT EXISTS senders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		full_name TEXT,
		email TEXT UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Progress tablosu
	createProgressTable := `
	CREATE TABLE IF NOT EXISTS scan_progress (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_processed_uid INTEGER DEFAULT 0,
		total_messages INTEGER DEFAULT 0,
		processed_count INTEGER DEFAULT 0,
		last_scan_date DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// İndeksler
	createIndexes := `
	CREATE INDEX IF NOT EXISTS idx_senders_email ON senders(email);
	CREATE INDEX IF NOT EXISTS idx_senders_created_at ON senders(created_at);`

	if _, err = db.Exec(createSendersTable); err != nil {
		return nil, err
	}
	if _, err = db.Exec(createProgressTable); err != nil {
		return nil, err
	}
	if _, err = db.Exec(createIndexes); err != nil {
		return nil, err
	}

	// İlk progress kaydını oluştur
	_, err = db.Exec(`INSERT OR IGNORE INTO scan_progress (id) VALUES (1)`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Progress bilgisini yükle
func loadProgress(db *sql.DB) (*Progress, error) {
	var progress Progress
	row := db.QueryRow(`
		SELECT last_processed_uid, total_messages, processed_count 
		FROM scan_progress WHERE id = 1`)

	err := row.Scan(&progress.LastProcessedUID, &progress.TotalMessages, &progress.ProcessedCount)
	if err != nil {
		return nil, err
	}

	progress.StartTime = time.Now()
	return &progress, nil
}

// Progress bilgisini kaydet
func saveProgress(db *sql.DB, progress *Progress) error {
	_, err := db.Exec(`
		UPDATE scan_progress 
		SET last_processed_uid = ?, total_messages = ?, processed_count = ?, last_scan_date = CURRENT_TIMESTAMP
		WHERE id = 1`,
		progress.LastProcessedUID, progress.TotalMessages, progress.ProcessedCount)
	return err
}

// E-posta adresinden isim çıkarma fonksiyonu
func extractNameFromEmail(emailAddr string) string {
	parts := strings.Split(emailAddr, "@")
	if len(parts) == 0 {
		return ""
	}

	username := parts[0]
	namePattern := regexp.MustCompile(`[._-]+`)
	nameParts := namePattern.Split(username, -1)

	var cleanParts []string
	for _, part := range nameParts {
		if part != "" {
			cleanParts = append(cleanParts, strings.Title(strings.ToLower(part)))
		}
	}

	return strings.Join(cleanParts, " ")
}

// Gönderici bilgilerini parse etme
func parseSender(fromHeader string) EmailSender {
	addr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		log.Printf("Adres parse edilemedi: %v", err)
		return EmailSender{}
	}

	email := strings.ToLower(addr.Address)
	fullName := ""

	if addr.Name != "" {
		fullName = strings.TrimSpace(addr.Name)
	} else {
		fullName = extractNameFromEmail(email)
	}

	log.Printf("Gönderici parse edildi: %s <%s>", fullName, email)

	return EmailSender{
		FullName: fullName,
		Email:    email,
	}
}

// Batch olarak veritabanına kaydetme
func saveSendersBatch(db *sql.DB, senders []EmailSender, verbose bool) error {
	if len(senders) == 0 {
		return nil
	}

	log.Printf("Batch kaydetme başlıyor: %d gönderici", len(senders))

	tx, err := db.Begin()
	if err != nil {
		log.Printf("Transaction başlatılamadı: %v", err)
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO senders (full_name, email) VALUES (?, ?)`)
	if err != nil {
		log.Printf("Statement hazırlanamadı: %v", err)
		return err
	}
	defer stmt.Close()

	savedCount := 0
	for _, sender := range senders {
		result, err := stmt.Exec(sender.FullName, sender.Email)
		if err != nil {
			log.Printf("Kaydetme hatası (%s): %v", sender.Email, err)
		} else {
			if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
				savedCount++
				if verbose {
					log.Printf("Yeni gönderici kaydedildi: %s <%s>", sender.FullName, sender.Email)
				}
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Transaction commit hatası: %v", err)
		return err
	}

	log.Printf("Batch kaydetme tamamlandı: %d/%d yeni kayıt", savedCount, len(senders))
	return nil
}

// E-posta adresinin zaten var olup olmadığını kontrol et
func emailExists(db *sql.DB, email string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM senders WHERE email = ?", email).Scan(&count)
	return err == nil && count > 0
}

// Batch işleme fonksiyonu
func processBatch(c *client.Client, startUID, endUID uint32) ([]EmailSender, error) {
	log.Printf("Batch işleniyor: UID %d-%d", startUID, endUID)

	seqset := new(imap.SeqSet)
	seqset.AddRange(startUID, endUID)

	section := &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{},
		Peek:         true,
	}

	items := []imap.FetchItem{section.FetchItem()}
	messages := make(chan *imap.Message, 50)

	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	var senders []EmailSender
	senderMap := make(map[string]EmailSender)
	processedCount := 0

	for msg := range messages {
		processedCount++

		r := msg.GetBody(section)
		if r == nil {
			log.Printf("Mesaj %d: Body alınamadı", msg.SeqNum)
			continue
		}

		entity, err := message.Read(r)
		if err != nil {
			log.Printf("Mesaj %d: Parse edilemedi: %v", msg.SeqNum, err)
			continue
		}

		fromHeader := entity.Header.Get("From")
		if fromHeader == "" {
			log.Printf("Mesaj %d: From header yok", msg.SeqNum)
			continue
		}

		sender := parseSender(fromHeader)
		if sender.Email == "" {
			log.Printf("Mesaj %d: Email parse edilemedi", msg.SeqNum)
			continue
		}

		// Duplicate kontrolü
		if _, exists := senderMap[sender.Email]; !exists {
			senderMap[sender.Email] = sender
		}
	}

	if err := <-done; err != nil {
		log.Printf("Batch fetch hatası: %v", err)
		return nil, err
	}

	// Map'i slice'a dönüştür
	for _, sender := range senderMap {
		senders = append(senders, sender)
	}

	log.Printf("Batch tamamlandı: %d mesaj işlendi, %d benzersiz gönderici bulundu", processedCount, len(senders))
	return senders, nil
}

// Batch işleme ile e-posta tarama
func scanEmailsBatch(config *Config, db *sql.DB) error {
	log.Printf("E-posta tarama başlıyor...")

	// Progress bilgisini yükle
	progress, err := loadProgress(db)
	if err != nil {
		log.Printf("Progress yüklenemedi: %v", err)
		return fmt.Errorf("Progress yüklenemedi: %v", err)
	}

	// IMAP bağlantısı
	log.Printf("IMAP sunucusuna bağlanılıyor: %s", config.IMAPServer)
	c, err := client.DialTLS(config.IMAPServer, &tls.Config{})
	if err != nil {
		log.Printf("IMAP bağlantısı başarısız: %v", err)
		return fmt.Errorf("IMAP bağlantısı başarısız: %v", err)
	}
	defer c.Logout()

	log.Printf("Kullanıcı girişi yapılıyor: %s", config.Username)
	if err := c.Login(config.Username, config.Password); err != nil {
		log.Printf("Giriş başarısız: %v", err)
		return fmt.Errorf("Giriş başarısız: %v", err)
	}

	log.Printf("INBOX seçiliyor...")
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("INBOX seçilemedi: %v", err)
		return fmt.Errorf("INBOX seçilemedi: %v", err)
	}

	log.Printf("Toplam mesaj sayısı: %d", mbox.Messages)
	if config.ShowProgress {
		fmt.Printf("Toplam mesaj sayısı: %d\n", mbox.Messages)
	}

	// Progress güncelle
	progress.TotalMessages = mbox.Messages

	if mbox.Messages == 0 {
		log.Printf("Hiç mesaj yok")
		if config.ShowProgress {
			fmt.Println("Hiç mesaj yok")
		}
		return nil
	}

	// Kaldığı yerden devam et
	startUID := progress.LastProcessedUID + 1
	if startUID > mbox.Messages {
		log.Printf("Tüm mesajlar zaten işlenmiş")
		if config.ShowProgress {
			fmt.Println("Tüm mesajlar zaten işlenmiş")
		}
		return nil
	}

	log.Printf("İşleme başlanıyor: UID %d'den itibaren", startUID)
	log.Printf("Daha önce işlenen mesaj sayısı: %d", progress.ProcessedCount)

	if config.ShowProgress {
		fmt.Printf("İşleme başlanıyor... (UID: %d'den itibaren)\n", startUID)
		fmt.Printf("Daha önce işlenen mesaj sayısı: %d\n", progress.ProcessedCount)
	}

	// Batch işleme döngüsü
	for currentUID := startUID; currentUID <= mbox.Messages; currentUID += uint32(config.BatchSize) {
		// Batch range hesapla
		endUID := currentUID + uint32(config.BatchSize) - 1
		if endUID > mbox.Messages {
			endUID = mbox.Messages
		}

		log.Printf("Batch işleniyor: %d-%d (%d/%d)", currentUID, endUID, endUID, mbox.Messages)
		if config.ShowProgress {
			fmt.Printf("Batch işleniyor: %d-%d (%d/%d)\n", currentUID, endUID, endUID, mbox.Messages)
		}

		// Batch'i işle
		senders, err := processBatch(c, currentUID, endUID)
		if err != nil {
			log.Printf("Batch işleme hatası: %v", err)
			// Hata durumunda progress'i kaydet ve devam et
			progress.LastProcessedUID = currentUID - 1
			saveProgress(db, progress)
			continue
		}

		log.Printf("Batch'ten %d benzersiz gönderici bulundu", len(senders))

		// Yeni gönderileri filtrele (veritabanında olmayanları)
		var newSenders []EmailSender
		for _, sender := range senders {
			if !emailExists(db, sender.Email) {
				newSenders = append(newSenders, sender)
			}
		}

		log.Printf("Yeni gönderici sayısı: %d", len(newSenders))

		// Veritabanına kaydet
		if len(newSenders) > 0 {
			if err := saveSendersBatch(db, newSenders, config.Verbose); err != nil {
				log.Printf("Batch kaydetme hatası: %v", err)
			} else if config.ShowProgress {
				fmt.Printf("Yeni gönderici sayısı: %d\n", len(newSenders))
			}
		}

		// Progress güncelle
		progress.LastProcessedUID = endUID
		progress.ProcessedCount = endUID
		if err := saveProgress(db, progress); err != nil {
			log.Printf("Progress kaydetme hatası: %v", err)
		}

		// İlerleme raporu
		if config.ShowProgress {
			elapsed := time.Since(progress.StartTime)
			remaining := time.Duration(float64(elapsed) * float64(mbox.Messages-endUID) / float64(endUID-startUID+1))
			fmt.Printf("İlerleme: %.2f%% - Geçen süre: %v - Tahmini kalan: %v\n",
				float64(endUID)/float64(mbox.Messages)*100, elapsed.Round(time.Second), remaining.Round(time.Second))

			log.Printf("İlerleme: %.2f%% - Geçen süre: %v - Tahmini kalan: %v",
				float64(endUID)/float64(mbox.Messages)*100, elapsed.Round(time.Second), remaining.Round(time.Second))
		}

		// Sunucuya fazla yük vermemek için kısa mola
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("Tarama tamamlandı!")
	if config.ShowProgress {
		fmt.Println("Tarama tamamlandı!")
	}
	return nil
}

// İstatistik gösterme
func showStats(db *sql.DB, username string) {
	log.Printf("İstatistikler gösteriliyor...")

	var totalSenders int
	db.QueryRow("SELECT COUNT(*) FROM senders").Scan(&totalSenders)

	var progress Progress
	db.QueryRow(`SELECT last_processed_uid, total_messages, processed_count FROM scan_progress WHERE id = 1`).
		Scan(&progress.LastProcessedUID, &progress.TotalMessages, &progress.ProcessedCount)

	log.Printf("Toplam benzersiz gönderici: %d", totalSenders)
	log.Printf("İşlenen mesaj sayısı: %d/%d", progress.ProcessedCount, progress.TotalMessages)

	fmt.Printf("\n=== İSTATİSTİKLER (%s) ===\n", username)
	fmt.Printf("Toplam benzersiz gönderici: %d\n", totalSenders)
	fmt.Printf("İşlenen mesaj sayısı: %d/%d\n", progress.ProcessedCount, progress.TotalMessages)
	if progress.TotalMessages > 0 {
		completion := float64(progress.ProcessedCount) / float64(progress.TotalMessages) * 100
		fmt.Printf("Tamamlanma oranı: %.2f%%\n", completion)
		log.Printf("Tamamlanma oranı: %.2f%%", completion)
	}

	// Son eklenen gönderenler
	fmt.Printf("\nSon eklenen göndereler:\n")
	rows, err := db.Query("SELECT full_name, email FROM senders ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		log.Printf("Son gönderenler sorgulanamadı: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var fullName, email string
		rows.Scan(&fullName, &email)
		fmt.Printf("  - %s <%s>\n", fullName, email)
		count++
	}

	log.Printf("Son %d gönderici listelendi", count)
}

func main() {
	// Komut satırı parametrelerini parse et
	config := parseFlags()

	// Log sistemini kur
	setupLogging(config)

	// CLI çıktısı (sadece temel bilgiler)
	fmt.Println("📧 E-POSTA TARAYICI")
	fmt.Printf("Kullanıcı: %s\n", config.Username)
	fmt.Printf("Sunucu: %s\n", config.IMAPServer)
	fmt.Printf("Veritabanı: %s\n", config.DBPath)
	fmt.Printf("Log dosyası: %s\n", config.LogPath)
	fmt.Printf("Batch boyutu: %d\n", config.BatchSize)

	// Veritabanını başlat
	db, err := initDB(config.DBPath)
	if err != nil {
		log.Printf("Veritabanı başlatılamadı: %v", err)
		fmt.Printf("❌ Veritabanı hatası: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	log.Printf("Veritabanı başlatıldı: %s", config.DBPath)

	// Mevcut durumu göster
	showStats(db, config.Username)

	fmt.Println("\n🚀 E-posta taraması başlıyor...")
	fmt.Println("📋 Detaylı loglar:", config.LogPath)

	// E-postaları tara
	if err := scanEmailsBatch(config, db); err != nil {
		log.Printf("E-posta tarama hatası: %v", err)
		fmt.Printf("❌ Tarama hatası: %v\n", err)
		fmt.Println("💡 Script kaldığı yerden devam edebilir. Tekrar çalıştırın.")
		os.Exit(1)
	}

	// Final istatistikleri göster
	showStats(db, config.Username)

	log.Printf("=== TARAMA TAMAMLANDI ===")
	fmt.Println("✅ Tarama başarıyla tamamlandı!")
}
