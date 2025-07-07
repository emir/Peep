package main

import (
	"crypto/tls"
	"fmt"
	"log"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func main() {
	// Test bilgileri
	imapServer := "imap.gmail.com:993"
	username := ""
	password := ""

	fmt.Println("Gmail IMAP bağlantısı test ediliyor...")
	fmt.Printf("Sunucu: %s\n", imapServer)
	fmt.Printf("Kullanıcı: %s\n", username)

	// Bağlantı test et
	c, err := client.DialTLS(imapServer, &tls.Config{})
	if err != nil {
		log.Fatal("IMAP bağlantısı başarısız:", err)
	}
	defer c.Logout()

	fmt.Println("✅ Sunucuya bağlandı")

	// Giriş test et
	if err := c.Login(username, password); err != nil {
		log.Fatal("❌ Giriş başarısız:", err)
	}

	fmt.Println("✅ Giriş başarılı!")

	// Mailbox listele
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	fmt.Println("📁 Klasörler:")
	for m := range mailboxes {
		fmt.Printf("  - %s\n", m.Name)
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}

	fmt.Println("🎉 Test başarılı! Ana script'i çalıştırabilirsiniz.")
}
