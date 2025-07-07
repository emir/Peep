# Peep 👀

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub release](https://img.shields.io/github/release/emir/Peep.svg)](https://github.com/emir/Peep/releases)
[![GitHub stars](https://img.shields.io/github/stars/emir/Peep.svg?style=social&label=Star)](https://github.com/emir/Peep)
[![GitHub forks](https://img.shields.io/github/forks/emir/Peep.svg?style=social&label=Fork)](https://github.com/emir/Peep/fork)

**Peep** is a powerful Go-based email scanner that extracts sender information from your IMAP inbox and stores it in a local SQLite database. Perfect for building contact lists, analyzing email patterns, or discovering who's been emailing you.

## ✨ Features

- 📧 **Multi-Provider Support** - Works with Gmail, Outlook, Yahoo, and any IMAP server
- 🗄️ **Local SQLite Storage** - Keeps your data private with automatic deduplication
- ⚡ **Batch Processing** - Efficiently handles large mailboxes (thousands of emails)
- 🔄 **Resume Capability** - Automatically resumes from where it left off
- 📊 **Real-time Progress** - Shows progress with time estimates
- 📁 **User Isolation** - Each email account gets its own folder and database
- 🛡️ **Status Tracking** - Monitor scan status via simple text files

## 🚀 Quick Start

### Prerequisites
- Go 1.24 or higher
- IMAP access to your email account

### Installation

```bash
# Clone the repository
git clone https://github.com/emir/Peep.git
cd Peep

# Install dependencies
go mod tidy

# Start scanning
go run main.go -user your-email@gmail.com -pass your-app-password
```

### Gmail Setup (Recommended)

For Gmail users, you'll need an **App Password**:

1. Enable [2-Step Verification](https://myaccount.google.com/security)
2. Go to **Security** → **App passwords**
3. Generate a new app password for "Mail"
4. Use this 16-character password with Peep

## 📖 Usage

### Basic Scanning
```bash
# Scan Gmail inbox
go run main.go -user john@gmail.com -pass abcdefghijklmnop

# Scan Outlook inbox
go run main.go -user john@outlook.com -pass mypassword -server outlook.office365.com:993

# Use smaller batches for slower connections
go run main.go -user john@gmail.com -pass mypass -batch 200
```

### Command Line Options

#### Main Scanner (`main.go`)
| Option | Default | Description |
|--------|---------|-------------|
| `-user` | - | **Required.** Your email address |
| `-pass` | - | **Required.** Your email password or app password |
| `-server` | `imap.gmail.com:993` | IMAP server address |
| `-batch` | `500` | Batch size (100-2000) |
| `-verbose` | `false` | Enable detailed logging |
| `-help` | `false` | Show help message |

## 📁 File Structure

Peep organizes data by user to support multiple email accounts:

```
./users/
├── john_at_gmail_com/
│   ├── database.db           # SQLite database with senders
│   ├── log_2025-01-07.txt    # Daily log file
│   └── status.txt            # Current scan status
└── mary_at_outlook_com/
    ├── database.db
    ├── log_2025-01-07.txt
    └── status.txt
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

-- Progress tracking for resume capability
CREATE TABLE scan_progress (
    id INTEGER PRIMARY KEY,
    last_processed_uid INTEGER,
    total_messages INTEGER,
    processed_count INTEGER,
    last_scan_date DATETIME
);
```

### Status File Format
```
STATUS: SUCCESS
TIME: 2025-01-07 14:30:15
MESSAGE: Scanning completed successfully. Found 150 unique senders.
```

Possible statuses: `RUNNING`, `SUCCESS`, `ERROR`

## 🖥️ Sample Output

```
📧 EMAIL SENDER SCANNER
User: john@gmail.com
Server: imap.gmail.com:993
Database: ./users/john_at_gmail_com/database.db
Status file: ./users/john_at_gmail_com/status.txt
Batch size: 500

=== STATISTICS (john@gmail.com) ===
Total unique senders: 0
Processed messages: 0/0

🚀 Email scanning started...

Total messages: 1,247
Starting processing... (from UID: 1)
Processing batch: 1-500 (500/1247)
New senders saved: 45
Progress: 40.1% - Elapsed: 2m15s - Estimated remaining: 3m20s
Processing batch: 501-1000 (1000/1247)
New senders saved: 32
Progress: 80.2% - Elapsed: 4m30s - Estimated remaining: 1m10s
...
✅ Scanning completed successfully!

=== STATISTICS (john@gmail.com) ===
Total unique senders: 127
Processed messages: 1247/1247
Completion rate: 100.00%

Recently added senders:
  - GitHub <noreply@github.com>
  - John Doe <john.doe@company.com>
  - Newsletter <news@website.com>
```

## 🌐 IMAP Server Support

| Provider | IMAP Server | Port | Notes |
|----------|-------------|------|--------|
| Gmail | `imap.gmail.com` | 993 | Requires app password |
| Outlook/Hotmail | `outlook.office365.com` | 993 | |
| Yahoo | `imap.mail.yahoo.com` | 993 | |
| Apple iCloud | `imap.mail.me.com` | 993 | |
| Custom | Your server | 993 | Most IMAP servers |

## 🔧 Monitoring and Automation

### Check Status Programmatically

**Bash Script:**
```bash
#!/bin/bash
USERNAME="john@gmail.com"
SAFE_USERNAME=$(echo "$USERNAME" | sed 's/@/_at_/g' | sed 's/\./_/g')
STATUS_FILE="./users/$SAFE_USERNAME/status.txt"

if [ -f "$STATUS_FILE" ]; then
    STATUS=$(grep "STATUS:" "$STATUS_FILE" | cut -d' ' -f2)
    echo "📧 $USERNAME status: $STATUS"
    cat "$STATUS_FILE"
else
    echo "❌ Status file not found for $USERNAME"
fi
```

**Python Script:**
```python
#!/usr/bin/env python3
import os

def check_status(email):
    safe_username = email.replace('@', '_at_').replace('.', '_')
    status_file = f"./users/{safe_username}/status.txt"
    
    if os.path.exists(status_file):
        with open(status_file, 'r') as f:
            content = f.read()
        print(f"📧 {email} status:")
        print(content)
    else:
        print(f"❌ Status file not found for {email}")

check_status("john@gmail.com")
```

## 🛠️ Troubleshooting

### Common Issues

**Authentication Failed**
- Use app passwords for Gmail (not your regular password)
- Enable IMAP in your email settings
- Check 2FA requirements

**Connection Timeout**
- Reduce batch size: `-batch 100`
- Check firewall settings
- Verify server address and port

**Performance Issues**
- Use larger batches for fast connections: `-batch 1000`
- Use smaller batches for slow connections: `-batch 100`
- Monitor progress in log files

### Getting Help

Check the detailed logs:
```bash
# View recent logs
tail -f ./users/your_username/log_2025-01-07.txt

# Search for errors
grep -i "error\|failed" ./users/your_username/log_2025-01-07.txt
```

## 📊 Performance

- **Small mailboxes** (< 1,000 emails): ~1-2 minutes
- **Medium mailboxes** (1,000-10,000 emails): ~5-15 minutes
- **Large mailboxes** (10,000+ emails): ~30+ minutes

Performance depends on:
- Internet connection speed
- IMAP server response time
- Batch size configuration
- Number of unique senders

## 🔒 Privacy & Security

- **Local storage only** - All data stays on your machine
- **No data transmission** - Senders info never leaves your computer
- **App passwords** - Secure authentication method
- **Read-only access** - Peep only reads emails, never modifies them

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [go-imap](https://github.com/emersion/go-imap) - Excellent IMAP library for Go
- [go-message](https://github.com/emersion/go-message) - Email message parsing
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure Go SQLite driver

---

<div align="center">

**[⭐ Star this repo](https://github.com/emir/Peep)** if you find it useful!

Made with ❤️ by [Emir](https://github.com/emir)

</div>