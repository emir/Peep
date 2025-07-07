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

// EmailSender structure
type EmailSender struct {
	FullName string
	Email    string
}

// Progress structure for tracking scan progress
type Progress struct {
	LastProcessedUID uint32
	TotalMessages    uint32
	ProcessedCount   uint32
	StartTime        time.Time
}

// Config structure
type Config struct {
	IMAPServer   string
	Username     string
	Password     string
	DBPath       string
	LogPath      string
	StatusPath   string
	BatchSize    int
	ShowProgress bool
	ShowHelp     bool
	Verbose      bool
}

// Parse command line arguments
func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.IMAPServer, "server", "imap.gmail.com:993", "IMAP server address")
	flag.StringVar(&config.Username, "user", "", "Email username (required)")
	flag.StringVar(&config.Password, "pass", "", "Email password (required)")
	flag.StringVar(&config.DBPath, "db", "", "Database file path (automatic)")
	flag.StringVar(&config.LogPath, "log", "", "Log file path (automatic)")
	flag.StringVar(&config.StatusPath, "status", "", "Status file path (automatic)")
	flag.IntVar(&config.BatchSize, "batch", 500, "Batch size (100-2000)")
	flag.BoolVar(&config.ShowProgress, "progress", true, "Show progress information")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&config.ShowHelp, "help", false, "Show help message")

	flag.Parse()

	if config.ShowHelp {
		showUsage()
		os.Exit(0)
	}

	if config.Username == "" || config.Password == "" {
		fmt.Println("❌ Error: -user and -pass parameters are required!")
		showUsage()
		os.Exit(1)
	}

	// Create safe folder name
	safeUsername := strings.ReplaceAll(config.Username, "@", "_at_")
	safeUsername = strings.ReplaceAll(safeUsername, ".", "_")
	safeUsername = strings.ReplaceAll(safeUsername, "+", "_plus_")

	// User-based folder structure
	userDir := filepath.Join("./users", safeUsername)
	os.MkdirAll(userDir, 0755)

	// Set file paths
	if config.DBPath == "" {
		config.DBPath = filepath.Join(userDir, "database.db")
	}

	if config.LogPath == "" {
		timestamp := time.Now().Format("2006-01-02")
		config.LogPath = filepath.Join(userDir, fmt.Sprintf("log_%s.txt", timestamp))
	}

	if config.StatusPath == "" {
		config.StatusPath = filepath.Join(userDir, "status.txt")
	}

	if config.BatchSize < 100 || config.BatchSize > 2000 {
		config.BatchSize = 500
	}

	return config
}

// Show usage information
func showUsage() {
	fmt.Println(`
📧 EMAIL SENDER SCANNER

USAGE:
  go run main.go -user <email> -pass <password> [options]

REQUIRED PARAMETERS:
  -user <email>     Email address
  -pass <password>  Email password (Gmail app password recommended)

OPTIONS:
  -server <server>  IMAP server address (default: imap.gmail.com:993)
  -db <path>        Database file path (auto: ./users/{username}/database.db)
  -log <path>       Log file path (auto: ./users/{username}/log_{date}.txt)
  -status <path>    Status file path (auto: ./users/{username}/status.txt)
  -batch <size>     Batch size 100-2000 (default: 500)
  -progress <bool>  Show progress information (default: true)
  -verbose          Enable verbose logging
  -help             Show this help message

EXAMPLES:
  go run main.go -user john@gmail.com -pass abcdefghijklmnop
  go run main.go -user john@outlook.com -pass mypass -server outlook.office365.com:993
  go run main.go -user john@gmail.com -pass mypass -batch 100 -verbose

FOLDER STRUCTURE:
  ./users/
  ├── john_at_gmail_com/
  │   ├── database.db
  │   ├── log_2025-01-07.txt
  │   └── status.txt
  └── mary_at_outlook_com/
      ├── database.db
      ├── log_2025-01-07.txt
      └── status.txt
`)
}

// Write status to file
func writeStatus(statusPath, status, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	content := fmt.Sprintf("STATUS: %s\nTIME: %s\nMESSAGE: %s\n", status, timestamp, message)

	if err := os.WriteFile(statusPath, []byte(content), 0644); err != nil {
		log.Printf("Failed to write status file: %v", err)
	}
}

// Setup logging system
func setupLogging(config *Config) {
	logFile, err := os.OpenFile(config.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("❌ Failed to create log file: %v\n", err)
		os.Exit(1)
	}

	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Initial log entry
	log.Printf("=== NEW SCAN STARTED ===")
	log.Printf("User: %s", config.Username)
	log.Printf("Server: %s", config.IMAPServer)
	log.Printf("Database: %s", config.DBPath)
	log.Printf("Batch size: %d", config.BatchSize)
}

// Initialize database
func initDB(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Senders table
	createSendersTable := `
	CREATE TABLE IF NOT EXISTS senders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		full_name TEXT,
		email TEXT UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Progress table
	createProgressTable := `
	CREATE TABLE IF NOT EXISTS scan_progress (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_processed_uid INTEGER DEFAULT 0,
		total_messages INTEGER DEFAULT 0,
		processed_count INTEGER DEFAULT 0,
		last_scan_date DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Indexes
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

	// Create initial progress record
	_, err = db.Exec(`INSERT OR IGNORE INTO scan_progress (id) VALUES (1)`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Load progress information
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

// Save progress information
func saveProgress(db *sql.DB, progress *Progress) error {
	_, err := db.Exec(`
		UPDATE scan_progress 
		SET last_processed_uid = ?, total_messages = ?, processed_count = ?, last_scan_date = CURRENT_TIMESTAMP
		WHERE id = 1`,
		progress.LastProcessedUID, progress.TotalMessages, progress.ProcessedCount)
	return err
}

// Extract name from email address
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

// Parse sender information
func parseSender(fromHeader string) EmailSender {
	addr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		log.Printf("Failed to parse address: %v", err)
		return EmailSender{}
	}

	email := strings.ToLower(addr.Address)
	fullName := ""

	if addr.Name != "" {
		fullName = strings.TrimSpace(addr.Name)
	} else {
		fullName = extractNameFromEmail(email)
	}

	log.Printf("Sender parsed: %s <%s>", fullName, email)

	return EmailSender{
		FullName: fullName,
		Email:    email,
	}
}

// Save senders in batch
func saveSendersBatch(db *sql.DB, senders []EmailSender, verbose bool) error {
	if len(senders) == 0 {
		return nil
	}

	log.Printf("Starting batch save: %d senders", len(senders))

	tx, err := db.Begin()
	if err != nil {
		log.Printf("Failed to start transaction: %v", err)
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO senders (full_name, email) VALUES (?, ?)`)
	if err != nil {
		log.Printf("Failed to prepare statement: %v", err)
		return err
	}
	defer stmt.Close()

	savedCount := 0
	for _, sender := range senders {
		result, err := stmt.Exec(sender.FullName, sender.Email)
		if err != nil {
			log.Printf("Save error (%s): %v", sender.Email, err)
		} else {
			if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
				savedCount++
				if verbose {
					log.Printf("New sender saved: %s <%s>", sender.FullName, sender.Email)
				}
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Transaction commit error: %v", err)
		return err
	}

	log.Printf("Batch save completed: %d/%d new records", savedCount, len(senders))
	return nil
}

// Check if email already exists
func emailExists(db *sql.DB, email string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM senders WHERE email = ?", email).Scan(&count)
	return err == nil && count > 0
}

// Process batch of messages
func processBatch(c *client.Client, startUID, endUID uint32) ([]EmailSender, error) {
	log.Printf("Processing batch: UID %d-%d", startUID, endUID)

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
			log.Printf("Message %d: Body not found", msg.SeqNum)
			continue
		}

		entity, err := message.Read(r)
		if err != nil {
			log.Printf("Message %d: Parse failed: %v", msg.SeqNum, err)
			continue
		}

		fromHeader := entity.Header.Get("From")
		if fromHeader == "" {
			log.Printf("Message %d: No From header", msg.SeqNum)
			continue
		}

		sender := parseSender(fromHeader)
		if sender.Email == "" {
			log.Printf("Message %d: Email parsing failed", msg.SeqNum)
			continue
		}

		// Duplicate check
		if _, exists := senderMap[sender.Email]; !exists {
			senderMap[sender.Email] = sender
		}
	}

	if err := <-done; err != nil {
		log.Printf("Batch fetch error: %v", err)
		return nil, err
	}

	// Convert map to slice
	for _, sender := range senderMap {
		senders = append(senders, sender)
	}

	log.Printf("Batch completed: %d messages processed, %d unique senders found", processedCount, len(senders))
	return senders, nil
}

// Scan emails with batch processing
func scanEmailsBatch(config *Config, db *sql.DB) error {
	log.Printf("Email scanning started...")

	// Load progress information
	progress, err := loadProgress(db)
	if err != nil {
		log.Printf("Failed to load progress: %v", err)
		return fmt.Errorf("failed to load progress: %v", err)
	}

	// IMAP connection
	log.Printf("Connecting to IMAP server: %s", config.IMAPServer)
	c, err := client.DialTLS(config.IMAPServer, &tls.Config{})
	if err != nil {
		log.Printf("IMAP connection failed: %v", err)
		return fmt.Errorf("IMAP connection failed: %v", err)
	}
	defer c.Logout()

	log.Printf("User login: %s", config.Username)
	if err := c.Login(config.Username, config.Password); err != nil {
		log.Printf("Login failed: %v", err)
		return fmt.Errorf("login failed: %v", err)
	}

	log.Printf("Selecting INBOX...")
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("Failed to select INBOX: %v", err)
		return fmt.Errorf("failed to select INBOX: %v", err)
	}

	log.Printf("Total messages: %d", mbox.Messages)
	if config.ShowProgress {
		fmt.Printf("Total messages: %d\n", mbox.Messages)
	}

	// Update progress
	progress.TotalMessages = mbox.Messages

	if mbox.Messages == 0 {
		log.Printf("No messages found")
		if config.ShowProgress {
			fmt.Println("No messages found")
		}
		return nil
	}

	// Resume from where it left off
	startUID := progress.LastProcessedUID + 1
	if startUID > mbox.Messages {
		log.Printf("All messages already processed")
		if config.ShowProgress {
			fmt.Println("All messages already processed")
		}
		return nil
	}

	log.Printf("Starting processing: from UID %d", startUID)
	log.Printf("Previously processed messages: %d", progress.ProcessedCount)

	if config.ShowProgress {
		fmt.Printf("Starting processing... (from UID: %d)\n", startUID)
		fmt.Printf("Previously processed messages: %d\n", progress.ProcessedCount)
	}

	// Batch processing loop
	for currentUID := startUID; currentUID <= mbox.Messages; currentUID += uint32(config.BatchSize) {
		// Calculate batch range
		endUID := currentUID + uint32(config.BatchSize) - 1
		if endUID > mbox.Messages {
			endUID = mbox.Messages
		}

		log.Printf("Processing batch: %d-%d (%d/%d)", currentUID, endUID, endUID, mbox.Messages)
		if config.ShowProgress {
			fmt.Printf("Processing batch: %d-%d (%d/%d)\n", currentUID, endUID, endUID, mbox.Messages)
		}

		// Process batch
		senders, err := processBatch(c, currentUID, endUID)
		if err != nil {
			log.Printf("Batch processing error: %v", err)
			// Save progress on error and continue
			progress.LastProcessedUID = currentUID - 1
			saveProgress(db, progress)
			continue
		}

		log.Printf("Found %d unique senders in batch", len(senders))

		// Filter new senders (not in database)
		var newSenders []EmailSender
		for _, sender := range senders {
			if !emailExists(db, sender.Email) {
				newSenders = append(newSenders, sender)
			}
		}

		log.Printf("New senders count: %d", len(newSenders))

		// Save to database
		if len(newSenders) > 0 {
			if err := saveSendersBatch(db, newSenders, config.Verbose); err != nil {
				log.Printf("Batch save error: %v", err)
			} else if config.ShowProgress {
				fmt.Printf("New senders saved: %d\n", len(newSenders))
			}
		}

		// Update progress
		progress.LastProcessedUID = endUID
		progress.ProcessedCount = endUID
		if err := saveProgress(db, progress); err != nil {
			log.Printf("Progress save error: %v", err)
		}

		// Progress report
		if config.ShowProgress {
			elapsed := time.Since(progress.StartTime)
			remaining := time.Duration(float64(elapsed) * float64(mbox.Messages-endUID) / float64(endUID-startUID+1))
			fmt.Printf("Progress: %.2f%% - Elapsed: %v - Estimated remaining: %v\n",
				float64(endUID)/float64(mbox.Messages)*100, elapsed.Round(time.Second), remaining.Round(time.Second))

			log.Printf("Progress: %.2f%% - Elapsed: %v - Estimated remaining: %v",
				float64(endUID)/float64(mbox.Messages)*100, elapsed.Round(time.Second), remaining.Round(time.Second))
		}

		// Brief pause to avoid overloading server
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("Scanning completed!")
	if config.ShowProgress {
		fmt.Println("Scanning completed!")
	}
	return nil
}

// Show statistics
func showStats(db *sql.DB, username string) {
	log.Printf("Showing statistics...")

	var totalSenders int
	db.QueryRow("SELECT COUNT(*) FROM senders").Scan(&totalSenders)

	var progress Progress
	db.QueryRow(`SELECT last_processed_uid, total_messages, processed_count FROM scan_progress WHERE id = 1`).
		Scan(&progress.LastProcessedUID, &progress.TotalMessages, &progress.ProcessedCount)

	log.Printf("Total unique senders: %d", totalSenders)
	log.Printf("Processed messages: %d/%d", progress.ProcessedCount, progress.TotalMessages)

	fmt.Printf("\n=== STATISTICS (%s) ===\n", username)
	fmt.Printf("Total unique senders: %d\n", totalSenders)
	fmt.Printf("Processed messages: %d/%d\n", progress.ProcessedCount, progress.TotalMessages)
	if progress.TotalMessages > 0 {
		completion := float64(progress.ProcessedCount) / float64(progress.TotalMessages) * 100
		fmt.Printf("Completion rate: %.2f%%\n", completion)
		log.Printf("Completion rate: %.2f%%", completion)
	}

	// Recently added senders
	fmt.Printf("\nRecently added senders:\n")
	rows, err := db.Query("SELECT full_name, email FROM senders ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		log.Printf("Failed to query recent senders: %v", err)
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

	log.Printf("Listed %d recent senders", count)
}

func main() {
	// Parse command line arguments
	config := parseFlags()

	// Setup logging system
	setupLogging(config)

	// Write initial status
	writeStatus(config.StatusPath, "RUNNING", "Email scanning started")

	// CLI output (basic information only)
	fmt.Println("📧 EMAIL SENDER SCANNER")
	fmt.Printf("User: %s\n", config.Username)
	fmt.Printf("Server: %s\n", config.IMAPServer)
	fmt.Printf("Database: %s\n", config.DBPath)
	fmt.Printf("Log file: %s\n", config.LogPath)
	fmt.Printf("Status file: %s\n", config.StatusPath)
	fmt.Printf("Batch size: %d\n", config.BatchSize)

	// Initialize database
	db, err := initDB(config.DBPath)
	if err != nil {
		errorMsg := fmt.Sprintf("Database error: %v", err)
		log.Printf("Failed to initialize database: %v", err)
		fmt.Printf("❌ %s\n", errorMsg)
		writeStatus(config.StatusPath, "ERROR", errorMsg)
		os.Exit(1)
	}
	defer db.Close()

	log.Printf("Database initialized: %s", config.DBPath)

	// Show current statistics
	showStats(db, config.Username)

	fmt.Println("\n🚀 Email scanning started...")
	fmt.Println("📋 Detailed logs:", config.LogPath)

	// Scan emails
	if err := scanEmailsBatch(config, db); err != nil {
		errorMsg := fmt.Sprintf("Scanning error: %v", err)
		log.Printf("Email scanning error: %v", err)
		fmt.Printf("❌ %s\n", errorMsg)
		fmt.Println("💡 Script can resume from where it left off. Run again.")
		writeStatus(config.StatusPath, "ERROR", errorMsg)
		os.Exit(1)
	}

	// Show final statistics
	showStats(db, config.Username)

	// Write success status
	var totalSenders int
	db.QueryRow("SELECT COUNT(*) FROM senders").Scan(&totalSenders)
	successMsg := fmt.Sprintf("Scanning completed successfully. Found %d unique senders.", totalSenders)

	log.Printf("=== SCANNING COMPLETED ===")
	fmt.Println("✅ Scanning completed successfully!")
	writeStatus(config.StatusPath, "SUCCESS", successMsg)
}
