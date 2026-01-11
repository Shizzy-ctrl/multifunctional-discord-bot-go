package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly/v2"
	"github.com/robfig/cron/v3"
)

type Config struct {
	Quotes    []string `json:"quotes"`
	ChannelID string   `json:"channel_id"`
}

var (
	config     Config
	configFile = "config.json"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Brak tokena Discord! Ustaw zmienną DISCORD_TOKEN")
	}

	rand.Seed(time.Now().UnixNano())

	loadConfig()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Błąd tworzenia sesji:", err)
	}

	dg.AddHandler(messageCreate)
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	go startCronScheduler(dg)

	err = dg.Open()
	if err != nil {
		log.Fatal("Błąd otwierania połączenia:", err)
	}
	defer dg.Close()

	fmt.Println("Bot działa! Codzienne cytaty o 9:00 CET. Naciśnij CTRL+C aby zakończyć.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func loadConfig() {
	data, err := os.ReadFile(configFile)
	if err != nil {
		config = Config{
			Quotes: []string{
				"Wytrwałość to klucz do sukcesu.",
				"Każdy dzień to nowa szansa.",
				"Wierz w siebie i swoje możliwości.",
			},
			ChannelID: "",
		}
		saveConfig()
		return
	}
	json.Unmarshal(data, &config)
}

func saveConfig() {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configFile, data, 0o644)
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	content := strings.TrimSpace(m.Content)

	if content == "!zlotamysl" || content == "!zm" {
		sendRandomQuote(s, m.ChannelID)
	} else if strings.HasPrefix(content, "!gem") {
		handleGemCommand(s, m)
	} else if strings.HasPrefix(content, "!dodaj ") {
		quote := strings.TrimPrefix(content, "!dodaj ")
		config.Quotes = append(config.Quotes, quote)
		saveConfig()
		s.ChannelMessageSend(m.ChannelID, "✅ Dodano nową złotą myśl!")
	} else if strings.HasPrefix(content, "!usun ") {
		numStr := strings.TrimPrefix(content, "!usun ")
		var num int
		fmt.Sscanf(numStr, "%d", &num)
		if num > 0 && num <= len(config.Quotes) {
			config.Quotes = append(config.Quotes[:num-1], config.Quotes[num:]...)
			saveConfig()
			s.ChannelMessageSend(m.ChannelID, "✅ Usunięto złotą myśl!")
		} else {
			s.ChannelMessageSend(m.ChannelID, "❌ Nieprawidłowy numer!")
		}
	} else if content == "!lista" {
		sendPaginatedList(s, m.ChannelID)
	} else if strings.HasPrefix(content, "!kanal ") {
		channelID := strings.TrimPrefix(content, "!kanal ")
		config.ChannelID = channelID
		saveConfig()
		s.ChannelMessageSend(m.ChannelID, "✅ Ustawiono kanał dla codziennych myśli!")
	} else if content == "!pomoc" {
		help := `**🌟 Złote Myśli Bot - Komendy:**

!zlotamysl lub !zm - Wyświetl losową złotą myśl
!dodaj <tekst> - Dodaj nową złotą myśl
!usun <numer> - Usuń złotą myśl (podaj numer z listy)
!lista - Pokaż wszystkie złote myśli
!kanal <ID> - Ustaw kanał dla codziennych myśli o 9:00
!gem [URL] - Pobierz wykres ze Stooq
!pomoc - Pokaż tę pomoc`
		s.ChannelMessageSend(m.ChannelID, help)
	}
}

func handleGemCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	urlStr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.Content), "!gem"))
	if urlStr == "" {
		urlStr = "https://stooq.pl/q/?s=eimi.uk&d=20260105&c=1y&t=l&a=lg&r=cndx.uk+cbu0.uk+ib01.uk"
	}

	s.ChannelMessageSend(m.ChannelID, "⏳ Pobieram wykres...")

	pngBytes, err := scrapeStooqChartPNG(urlStr)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "❌ Nie udało się pobrać wykresu: "+err.Error())
		return
	}

	_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Files: []*discordgo.File{
			{Name: "gem.png", ContentType: "image/png", Reader: bytes.NewReader(pngBytes)},
		},
	})
	if err != nil {
		log.Println("Błąd wysyłania pliku na Discord:", err)
	}
}

func scrapeStooqChartPNG(pageURL string) ([]byte, error) {
	log.Printf("Scrapowanie strony: %s", pageURL)

	var pngData []byte
	var scrapeErr error
	found := false

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	c.SetRequestTimeout(30 * time.Second)

	// Dodaj nagłówki
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "pl,en-US;q=0.7,en;q=0.3")
		r.Headers.Set("Referer", "https://stooq.pl/")
		log.Printf("Odwiedzam: %s", r.URL)
	})

	// Szukaj obrazków wykresu
	c.OnHTML("img", func(e *colly.HTMLElement) {
		if found {
			return
		}

		// Sprawdź różne atrybuty src
		imgSrc := e.Attr("src")
		if imgSrc == "" {
			imgSrc = e.Attr("src2")
		}
		if imgSrc == "" {
			return
		}

		// Sprawdź czy to wykres (po ID, klasie lub atrybutach rodzica)
		parent := e.DOM.Parent()
		parentID, _ := parent.Attr("id")
		parentClass, _ := parent.Attr("class")

		isChart := strings.Contains(parentID, "chart") ||
			strings.Contains(parentID, "aqi_mc") ||
			strings.Contains(parentClass, "chart") ||
			strings.Contains(imgSrc, "/q/c/") ||
			strings.HasPrefix(imgSrc, "data:image/png;base64,")

		if !isChart {
			return
		}

		log.Printf("Znaleziono kandydata na wykres: %s", imgSrc[:min(len(imgSrc), 100)])

		// Jeśli to base64, dekoduj
		if strings.HasPrefix(imgSrc, "data:image/png;base64,") {
			b64Data := strings.TrimPrefix(imgSrc, "data:image/png;base64,")
			decoded, err := base64.StdEncoding.DecodeString(b64Data)
			if err == nil && isPNG(decoded) {
				pngData = decoded
				found = true
				log.Printf("✅ Pomyślnie zdekodowano base64 PNG (%d bajtów)", len(pngData))
				return
			}
		}

		// Jeśli to URL, pobierz obrazek
		imgURL := e.Request.AbsoluteURL(imgSrc)
		log.Printf("Pobieranie obrazka z: %s", imgURL)

		// Stwórz nowy collector dla obrazka
		imgCollector := c.Clone()
		imgCollector.OnResponse(func(r *colly.Response) {
			if strings.Contains(r.Headers.Get("Content-Type"), "image") && isPNG(r.Body) {
				pngData = r.Body
				found = true
				log.Printf("✅ Pomyślnie pobrano PNG (%d bajtów)", len(pngData))
			}
		})

		imgCollector.Visit(imgURL)
	})

	c.OnError(func(r *colly.Response, err error) {
		scrapeErr = fmt.Errorf("błąd scrapowania: %w", err)
		log.Printf("❌ Błąd: %v", err)
	})

	err := c.Visit(pageURL)
	if err != nil {
		return nil, fmt.Errorf("błąd odwiedzania strony: %w", err)
	}

	c.Wait()

	if scrapeErr != nil {
		return nil, scrapeErr
	}

	if !found || len(pngData) == 0 {
		return nil, errors.New("nie znaleziono wykresu PNG na stronie")
	}

	return pngData, nil
}

func isPNG(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	return b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4e && b[3] == 0x47 &&
		b[4] == 0x0d && b[5] == 0x0a && b[6] == 0x1a && b[7] == 0x0a
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sendRandomQuote(s *discordgo.Session, channelID string) {
	if len(config.Quotes) == 0 {
		s.ChannelMessageSend(channelID, "Brak złotych myśli! Dodaj je komendą !dodaj")
		return
	}
	quote := config.Quotes[rand.Intn(len(config.Quotes))]
	s.ChannelMessageSend(channelID, fmt.Sprintf("✨ **Złota Myśl:** ✨\n\n*%s*", quote))
}

func startCronScheduler(s *discordgo.Session) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		log.Fatal("Location error:", err)
	}

	c := cron.New(cron.WithLocation(loc))

	_, err = c.AddFunc("0 9 * * ?", func() {
		fmt.Println("🕐 CRON 9:00 CET!")
		if config.ChannelID != "" {
			sendDailyQuote(s, config.ChannelID)
		}
	})
	if err != nil {
		log.Fatal("Cron AddFunc błąd:", err)
	}

	fmt.Println("✅ Cron działa - 9:00 CET codziennie!")
	c.Start()
}

func sendDailyQuote(s *discordgo.Session, channelID string) {
	if len(config.Quotes) == 0 {
		s.ChannelMessageSend(channelID, "Brak złotych myśli! Dodaj je komendą !dodaj")
		return
	}
	quote := config.Quotes[rand.Intn(len(config.Quotes))]
	s.ChannelMessageSend(channelID, fmt.Sprintf("🌅 **Złota myśl dnia** 🌅\n\n*%s*", quote))
}

func sendPaginatedList(s *discordgo.Session, channelID string) {
	if len(config.Quotes) == 0 {
		s.ChannelMessageSend(channelID, "Brak złotych myśli!")
		return
	}

	const maxChars = 1800
	const maxQuotesPerPage = 12

	for i := 0; i < len(config.Quotes); i += maxQuotesPerPage {
		end := i + maxQuotesPerPage
		if end > len(config.Quotes) {
			end = len(config.Quotes)
		}

		var msg strings.Builder
		msg.WriteString(fmt.Sprintf("**📜 Złote Myśli (%d-%d/%d):**\n\n", i+1, end, len(config.Quotes)))

		pageChars := 50
		for j := i; j < end; j++ {
			quoteNum := fmt.Sprintf("%d. ", j+1)
			quotePreview := config.Quotes[j]

			if len(quotePreview) > 100 {
				quotePreview = quotePreview[:97] + "..."
			}

			line := quoteNum + quotePreview + "\n"
			if pageChars+len(line) > maxChars {
				break
			}

			msg.WriteString(line)
			pageChars += len(line)
		}

		if _, err := s.ChannelMessageSend(channelID, msg.String()); err != nil {
			log.Println("Błąd wysyłania listy:", err)
			return
		}

		time.Sleep(1000 * time.Millisecond)
	}
}
