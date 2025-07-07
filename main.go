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
	FirstName string
	LastName  string
	Email     string
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
	BatchSize    int
	ShowProgress bool
	ShowHelp     bool
}

// Komut satırı parametrelerini parse et
func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.IMAPServer, "server", "imap.gmail.com:993", "IMAP sunucu adresi (örn: imap.gmail.com:993)")
	flag.StringVar(&config.Username, "user", "", "E-posta kullanıcı adı (zorunlu)")
	flag.StringVar(&config.Password, "pass", "", "E-posta şifresi veya uygulama şifresi (zorunlu)")
	flag.StringVar(&config.DBPath, "db", "", "Veritabanı dosya yolu (otomatik: ./db/{username}.db)")
	flag.IntVar(&config.BatchSize, "batch", 500, "Batch boyutu (100-2000)")
	flag.BoolVar(&config.ShowProgress, "progress", true, "İlerleme durumunu göster")
	flag.BoolVar(&config.ShowHelp, "help", false, "Yardım göster")

	flag.Parse()

	// Yardım kontrolü
	if config.ShowHelp {
		showUsage()
		os.Exit(0)
	}

	// Zorunlu parametreler kontrolü
	if config.Username == "" || config.Password == "" {
		fmt.Println("❌ Hata: -user ve -pass parametreleri zorunludur!")
		showUsage()
		os.Exit(1)
	}

	// Veritabanı yolu oluştur
	if config.DBPath == "" {
		// Username'den güvenli dosya adı oluştur
		safeUsername := strings.ReplaceAll(config.Username, "@", "_at_")
		safeUsername = strings.ReplaceAll(safeUsername, ".", "_")

		// db klasörü oluştur
		dbDir := "./db"
		os.MkdirAll(dbDir, 0755)

		config.DBPath = filepath.Join(dbDir, safeUsername+".db")
	}

	// Batch size kontrolü
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
  -user <email>     E-posta adresi (örn: john@gmail.com)
  -pass <password>  E-posta şifresi (Gmail için uygulama şifresi)

SEÇENEKLER:
  -server <server>  IMAP sunucu adresi (varsayılan: imap.gmail.com:993)
  -db <path>        Veritabanı dosya yolu (varsayılan: ./db/{username}.db)
  -batch <size>     Batch boyutu 100-2000 (varsayılan: 500)
  -progress <bool>  İlerleme göster (varsayılan: true)
  -help             Bu yardımı göster

ÖRNEKLER:
  # Gmail kullanıcısı için
  go run main.go -user john@gmail.com -pass abcdefghijklmnop

  # Farklı IMAP sunucusu ile
  go run main.go -user john@outlook.com -pass mypassword -server outlook.office365.com:993

  # Özel veritabanı yolu ile
  go run main.go -user john@gmail.com -pass mypass -db /path/to/custom.db

  # Küçük batch boyutu ile (yavaş internet için)
  go run main.go -user john@gmail.com -pass mypass -batch 100

NOT: Gmail için uygulama şifresi oluşturmanız gerekir!
`)
}

// Veritabanı yapılandırması
func initDB(dbPath string) (*sql.DB, error) {
	// Veritabanı klasörünü oluştur
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
		first_name TEXT,
		last_name TEXT,
		email TEXT UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Progress tablosu - kaldığı yerden devam için
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
func extractNameFromEmail(emailAddr string) (string, string) {
	parts := strings.Split(emailAddr, "@")
	if len(parts) == 0 {
		return "", ""
	}

	username := parts[0]
	namePattern := regexp.MustCompile(`[._-]+`)
	nameParts := namePattern.Split(username, -1)

	firstName := ""
	lastName := ""

	if len(nameParts) >= 1 {
		firstName = strings.Title(strings.ToLower(nameParts[0]))
	}
	if len(nameParts) >= 2 {
		lastName = strings.Title(strings.ToLower(nameParts[1]))
	}

	return firstName, lastName
}

// Gönderici bilgilerini parse etme
func parseSender(fromHeader string) EmailSender {
	addr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		log.Printf("Adres parse edilemedi: %v", err)
		return EmailSender{}
	}

	email := strings.ToLower(addr.Address)

	var firstName, lastName string

	if addr.Name != "" {
		nameParts := strings.Fields(addr.Name)
		if len(nameParts) >= 1 {
			firstName = nameParts[0]
		}
		if len(nameParts) >= 2 {
			lastName = strings.Join(nameParts[1:], " ")
		}
	} else {
		firstName, lastName = extractNameFromEmail(email)
	}

	return EmailSender{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
	}
}

// Batch olarak veritabanına kaydetme
func saveSendersBatch(db *sql.DB, senders []EmailSender) error {
	if len(senders) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO senders (first_name, last_name, email) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sender := range senders {
		_, err = stmt.Exec(sender.FirstName, sender.LastName, sender.Email)
		if err != nil {
			log.Printf("Kaydetme hatası (%s): %v", sender.Email, err)
		}
	}

	return tx.Commit()
}

// E-posta adresinin zaten var olup olmadığını kontrol et
func emailExists(db *sql.DB, email string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM senders WHERE email = ?", email).Scan(&count)
	return err == nil && count > 0
}

// Batch işleme ile e-posta tarama
func scanEmailsBatch(config *Config, db *sql.DB) error {
	// Progress bilgisini yükle
	progress, err := loadProgress(db)
	if err != nil {
		return fmt.Errorf("Progress yüklenemedi: %v", err)
	}

	// IMAP bağlantısı
	c, err := client.DialTLS(config.IMAPServer, &tls.Config{})
	if err != nil {
		return fmt.Errorf("IMAP bağlantısı başarısız: %v", err)
	}
	defer c.Logout()

	if err := c.Login(config.Username, config.Password); err != nil {
		return fmt.Errorf("Giriş başarısız: %v", err)
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return fmt.Errorf("INBOX seçilemedi: %v", err)
	}

	fmt.Printf("Toplam mesaj sayısı: %d\n", mbox.Messages)

	// Progress güncelle
	progress.TotalMessages = mbox.Messages

	if mbox.Messages == 0 {
		fmt.Println("Hiç mesaj yok")
		return nil
	}

	// Kaldığı yerden devam et
	startUID := progress.LastProcessedUID + 1
	if startUID > mbox.Messages {
		fmt.Println("Tüm mesajlar zaten işlenmiş")
		return nil
	}

	if config.ShowProgress {
		fmt.Printf("İşleme başlanıyor... (UID: %d'den itibaren)\n", startUID)
		fmt.Printf("Daha önce işlenen mesaj sayısı: %d\n", progress.ProcessedCount)
	}

	// Batch işleme
	for currentUID := startUID; currentUID <= mbox.Messages; currentUID += uint32(config.BatchSize) {
		// Batch range hesapla
		endUID := currentUID + uint32(config.BatchSize) - 1
		if endUID > mbox.Messages {
			endUID = mbox.Messages
		}

		if config.ShowProgress {
			fmt.Printf("\nBatch işleniyor: %d-%d (%d/%d)\n", currentUID, endUID, endUID, mbox.Messages)
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

		// Yeni gönderileri filtrele (veritabanında olmayanları)
		var newSenders []EmailSender
		for _, sender := range senders {
			if !emailExists(db, sender.Email) {
				newSenders = append(newSenders, sender)
			}
		}

		// Veritabanına kaydet
		if len(newSenders) > 0 {
			if err := saveSendersBatch(db, newSenders); err != nil {
				log.Printf("Batch kaydetme hatası: %v", err)
			} else if config.ShowProgress {
				fmt.Printf("Yeni gönderici sayısı: %d\n", len(newSenders))
				for _, sender := range newSenders {
					fmt.Printf("  + %s %s <%s>\n", sender.FirstName, sender.LastName, sender.Email)
				}
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
		}

		// Kısa bir mola (sunucuya fazla yük vermemek için)
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("\nTarama tamamlandı!")
	return nil
}

// Batch işleme fonksiyonu
func processBatch(c *client.Client, startUID, endUID uint32) ([]EmailSender, error) {
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

	for msg := range messages {
		r := msg.GetBody(section)
		if r == nil {
			continue
		}

		entity, err := message.Read(r)
		if err != nil {
			continue
		}

		fromHeader := entity.Header.Get("From")
		if fromHeader == "" {
			continue
		}

		sender := parseSender(fromHeader)
		if sender.Email == "" {
			continue
		}

		// Duplicate kontrolü
		if _, exists := senderMap[sender.Email]; !exists {
			senderMap[sender.Email] = sender
		}
	}

	if err := <-done; err != nil {
		return nil, err
	}

	// Map'i slice'a dönüştür
	for _, sender := range senderMap {
		senders = append(senders, sender)
	}

	return senders, nil
}

// İstatistik gösterme
func showStats(db *sql.DB, username string) {
	var totalSenders int
	db.QueryRow("SELECT COUNT(*) FROM senders").Scan(&totalSenders)

	var progress Progress
	db.QueryRow(`SELECT last_processed_uid, total_messages, processed_count FROM scan_progress WHERE id = 1`).
		Scan(&progress.LastProcessedUID, &progress.TotalMessages, &progress.ProcessedCount)

	fmt.Printf("\n=== İSTATİSTİKLER (%s) ===\n", username)
	fmt.Printf("Toplam benzersiz gönderici: %d\n", totalSenders)
	fmt.Printf("İşlenen mesaj sayısı: %d/%d\n", progress.ProcessedCount, progress.TotalMessages)
	if progress.TotalMessages > 0 {
		fmt.Printf("Tamamlanma oranı: %.2f%%\n", float64(progress.ProcessedCount)/float64(progress.TotalMessages)*100)
	}

	// Son eklenen gönderenler
	fmt.Printf("\nSon eklenen göndereler:\n")
	rows, err := db.Query("SELECT first_name, last_name, email FROM senders ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var firstName, lastName, email string
		rows.Scan(&firstName, &lastName, &email)
		fmt.Printf("  - %s %s <%s>\n", firstName, lastName, email)
	}
}

func main() {
	// Komut satırı parametrelerini parse et
	config := parseFlags()

	fmt.Println("=== E-POSTA TARAYICI ===")
	fmt.Printf("Kullanıcı: %s\n", config.Username)
	fmt.Printf("Sunucu: %s\n", config.IMAPServer)
	fmt.Printf("Veritabanı: %s\n", config.DBPath)
	fmt.Printf("Batch boyutu: %d\n", config.BatchSize)

	// Veritabanını başlat
	db, err := initDB(config.DBPath)
	if err != nil {
		log.Fatal("Veritabanı başlatılamadı:", err)
	}
	defer db.Close()

	// Mevcut durumu göster
	showStats(db, config.Username)

	fmt.Println("\nE-posta taraması başlıyor...")

	// E-postaları tara
	if err := scanEmailsBatch(config, db); err != nil {
		log.Printf("E-posta tarama hatası: %v", err)
		fmt.Println("Script kaldığı yerden devam edebilir. Tekrar çalıştırın.")
		os.Exit(1)
	}

	// Final istatistikleri göster
	showStats(db, config.Username)
}
