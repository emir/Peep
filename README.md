# Email Sender Scanner

A Go tool that scans your email inbox via IMAP and extracts sender information (names and email addresses) into a SQLite database. Perfect for building contact lists, analyzing your email patterns, or cleaning up your contacts.

## Features

- 📧 **IMAP Email Scanning** - Works with Gmail, Outlook, Yahoo, and other IMAP servers
- 🗄️ **SQLite Database** - Stores sender info locally with automatic deduplication
- ⚡ **Batch Processing** - Efficiently handles large mailboxes (thousands of emails)
- 🔄 **Resume Support** - Picks up where it left off if interrupted
- 📊 **Progress Tracking** - Shows real-time progress with time estimates
- 📝 **Detailed Logging** - Comprehensive logs for troubleshooting
- 🏷️ **Smart Name Extraction** - Extracts names from email headers or email addresses

## Installation

### Prerequisites
- Go 1.21+ installed
- IMAP access to your email account

### Setup
```bash
# Clone or download the code
mkdir peep
cd peep

# Create the files
# Copy main.go and go.mod to this directory

# Install dependencies
go mod tidy

# Run the scanner
go run main.go -user your-email@gmail.com -pass your-password
```

## Gmail Setup (Recommended)

For Gmail users, you'll need an **App Password** (not your regular password):

1. Go to your [Google Account settings](https://myaccount.google.com)
2. Navigate to **Security** → **2-Step Verification**
3. Enable 2-Step Verification if not already enabled
4. Go to **Security** → **App passwords**
5. Generate a new app password for "Mail"
6. Use this 16-character password with the tool

## Usage

### Basic Scanning
```bash
# Scan Gmail inbox
go run main.go -user john@gmail.com -pass abcdefghijklmnop

# Scan Outlook inbox
go run main.go -user john@outlook.com -pass mypassword -server outlook.office365.com:993

# Scan with custom batch size (for slower connections)
go run main.go -user john@gmail.com -pass mypass -batch 100
```

### Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `-user` | - | **Required.** Your email address |
| `-pass` | - | **Required.** Your email password or app password |
| `-server` | `imap.gmail.com:993` | IMAP server address |
| `-db` | `./db/{username}.db` | SQLite database file path |
| `-log` | `./logs/{username}_{date}.log` | Log file path |
| `-batch` | `500` | Number of emails to process at once (100-2000) |
| `-progress` | `true` | Show progress information |
| `-verbose` | `false` | Enable detailed logging |
| `-help` | `false` | Show help message |

### Examples

```bash
# Basic scan with default settings
go run main.go -user jane@gmail.com -pass myapppassword

# Scan with verbose logging
go run main.go -user jane@gmail.com -pass mypass -verbose

# Use smaller batches for slow connections
go run main.go -user jane@gmail.com -pass mypass -batch 200

# Custom database location
go run main.go -user jane@gmail.com -pass mypass -db /path/to/custom.db
```

## Output

### File Structure
```
./
├── main.go
├── go.mod
├── db/
│   └── jane_at_gmail_com.db    # Your SQLite database
└── logs/
    └── jane_at_gmail_com_2025-01-07.log  # Detailed logs
```

### Database Schema
```sql
-- Sender information
CREATE TABLE senders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    full_name TEXT,
    email TEXT UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Progress tracking
CREATE TABLE scan_progress (
    id INTEGER PRIMARY KEY,
    last_processed_uid INTEGER,
    total_messages INTEGER,
    processed_count INTEGER,
    last_scan_date DATETIME
);
```

### Sample Output
```
📧 E-POSTA TARAYICI
Kullanıcı: jane@gmail.com
Sunucu: imap.gmail.com:993
Veritabanı: ./db/jane_at_gmail_com.db
Log dosyası: ./logs/jane_at_gmail_com_2025-01-07.log
Batch boyutu: 500

=== İSTATİSTİKLER (jane@gmail.com) ===
Toplam benzersiz gönderici: 0
İşlenen mesaj sayısı: 0/0
Tamamlanma oranı: 0.00%

🚀 E-posta taraması başlıyor...
📋 Detaylı loglar: ./logs/jane_at_gmail_com_2025-01-07.log

Toplam mesaj sayısı: 1500
İşleme başlanıyor... (UID: 1'den itibaren)
Batch işleniyor: 1-500 (500/1500)
Yeni gönderici sayısı: 45
İlerleme: 33.33% - Geçen süre: 1m30s - Tahmini kalan: 3m0s
Batch işleniyor: 501-1000 (1000/1500)
Yeni gönderici sayısı: 32
İlerleme: 66.67% - Geçen süre: 3m0s - Tahmini kalan: 1m30s
...
✅ Tarama başarıyla tamamlandı!
```

## Common IMAP Servers

| Provider | IMAP Server | Port |
|----------|-------------|------|
| Gmail | `imap.gmail.com` | 993 |
| Outlook/Hotmail | `outlook.office365.com` | 993 |
| Yahoo | `imap.mail.yahoo.com` | 993 |
| Apple iCloud | `imap.mail.me.com` | 993 |

## Troubleshooting

### Common Issues

**Authentication Failed**
- Make sure you're using an app password for Gmail (not your regular password)
- Check if 2FA is enabled and required
- Verify IMAP is enabled in your email settings

**Connection Timeout**
- Try reducing batch size: `-batch 100`
- Check your firewall settings
- Verify the IMAP server address and port

**Permission Denied**
- Make sure the `./db` and `./logs` directories are writable
- Run with appropriate file permissions

### Getting Help

Check the log files for detailed error information:
```bash
# View recent logs
tail -f ./logs/your_email_2025-01-07.log

# Search for errors
grep -i "error\|failed" ./logs/your_email_2025-01-07.log
```

## Resume Functionality

The tool automatically saves progress and can resume if interrupted:

- **Progress is saved** after each batch
- **Run the same command again** to continue where you left off
- **No duplicate data** - existing emails won't be re-processed

## Performance Tips

- **Batch Size**: Use smaller batches (100-200) for slow connections, larger (1000+) for fast ones
- **Large Mailboxes**: The tool handles tens of thousands of emails efficiently
- **Network**: Stable internet connection recommended for large scans

## Security Notes

- Passwords are only used for IMAP connection (not stored)
- All data stays local on your machine
- SQLite database contains only sender names and email addresses
- Use app passwords instead of main account passwords when possible

## Dependencies

- `github.com/emersion/go-imap` - IMAP client
- `github.com/emersion/go-message` - Email message parsing
- `modernc.org/sqlite` - Pure Go SQLite driver

## License

This project is open source. Feel free to modify and distribute.