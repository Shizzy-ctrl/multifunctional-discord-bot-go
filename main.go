package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

func main() {
	godotenv.Load()

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Brak tokena Discord! Ustaw zmienną DISCORD_TOKEN")
	}

	rand.Seed(time.Now().UnixNano()) // ✅ Losowe cytaty

	loadConfig()

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Błąd tworzenia sesji:", err)
	}

	dg.AddHandler(messageCreate)
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	// 🚀 CRON SCHEDULER zamiast tickera
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

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	content := strings.TrimSpace(m.Content)

	if content == "!zlotamysl" || content == "!zm" {
		sendRandomQuote(s, m.ChannelID)
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
!gem - Wygeneruj wykres ETF jako PNG
!gemsubscribe - Zapisz się na miesięczny wykres ETF (ostatni dzień miesiąca, 10:00)
!pomoc - Pokaż tę pomoc`
		s.ChannelMessageSend(m.ChannelID, help)
	} else if content == "!gem" {
		statusMsg, statusErr := s.ChannelMessageSend(m.ChannelID, "⏳ Generuję wykres...")
		if err := generateAndSendGem(s, m.ChannelID); err != nil {
			log.Println("!gem error:", err)
			if statusErr == nil && statusMsg != nil {
				s.ChannelMessageDelete(m.ChannelID, statusMsg.ID)
			}
			s.ChannelMessageSend(m.ChannelID, "❌ Nie udało się wygenerować wykresu")
			return
		}
		if statusErr == nil && statusMsg != nil {
			s.ChannelMessageDelete(m.ChannelID, statusMsg.ID)
		}
	} else if content == "!gemsubscribe" {
		added := addGemSubscriber(m.Author.ID)
		config.GemChannelID = m.ChannelID
		saveConfig()
		if added {
			s.ChannelMessageSend(m.ChannelID, "✅ Zapisano na miesięczny wykres ETF. Ostatni dzień miesiąca o 10:00 wrzucę wykres i oznaczę zapisanych.")
		} else {
			s.ChannelMessageSend(m.ChannelID, "✅ Już jesteś zapisany. Ostatni dzień miesiąca o 10:00 wrzucę wykres i oznaczę zapisanych.")
		}
	} else if content == "!pogoda" {
		msg := buildTomorrowWeatherMessage()
		if msg == "" {
			s.ChannelMessageSend(m.ChannelID, "❌ Nie udało się pobrać prognozy")
			return
		}
		s.ChannelMessageSend(m.ChannelID, msg)
	}
}

func addGemSubscriber(userID string) bool {
	for _, id := range config.GemSubscribers {
		if id == userID {
			return false
		}
	}
	config.GemSubscribers = append(config.GemSubscribers, userID)
	return true
}

func isLastDayOfMonth(t time.Time) bool {
	nextDay := t.AddDate(0, 0, 1)
	return nextDay.Month() != t.Month()
}

func mentionGemSubscribers() string {
	if len(config.GemSubscribers) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range config.GemSubscribers {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString("<@")
		b.WriteString(id)
		b.WriteString(">")
	}
	return b.String()
}

func generateAndSendGem(s *discordgo.Session, channelID string) error {
	tmpDir := os.TempDir()
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("gem_%d.png", time.Now().UnixNano()))

	if err := generateGemChart(outputPath); err != nil {
		return err
	}

	defer os.Remove(outputPath)

	file, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = s.ChannelFileSend(channelID, "etfs_rok.png", file)
	return err
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
			// ZMIENIONO: "Złota myśl dnia" zamiast zwykłej złotej myśli
			sendDailyQuote(s, config.ChannelID)
		}
	})
	if err != nil {
		log.Fatal("Cron AddFunc błąd:", err)
	}

	_, err = c.AddFunc("0 10 * * *", func() {
		now := time.Now().In(loc)
		if !isLastDayOfMonth(now) {
			return
		}
		if config.GemChannelID == "" || len(config.GemSubscribers) == 0 {
			return
		}
		if msg := mentionGemSubscribers(); msg != "" {
			s.ChannelMessageSend(config.GemChannelID, msg)
		}
		if err := generateAndSendGem(s, config.GemChannelID); err != nil {
			log.Println("scheduled gem error:", err)
			s.ChannelMessageSend(config.GemChannelID, "❌ Nie udało się wygenerować wykresu")
		}
	})
	if err != nil {
		log.Fatal("Cron AddFunc błąd:", err)
	}

	_, err = c.AddFunc("0 19 * * *", func() {
		if config.GemChannelID == "" || len(config.GemSubscribers) == 0 {
			return
		}
		msg := buildTomorrowWeatherMessage()
		if msg == "" {
			return
		}
		mention := mentionGemSubscribers()
		if mention != "" {
			msg = mention + "\n" + msg
		}
		s.ChannelMessageSend(config.GemChannelID, msg)
	})
	if err != nil {
		log.Fatal("Cron AddFunc błąd:", err)
	}

	fmt.Println("✅ Cron działa - 9:00 CET codziennie!")
	c.Start()
}

// NOWA FUNKCJA dla zaplanowanej złotej myśli dnia
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

		// POPRAWIONE: _ dla message, err dla błędu
		if _, err := s.ChannelMessageSend(channelID, msg.String()); err != nil {
			log.Println("Błąd wysyłania listy:", err)
			return
		}

		time.Sleep(1000 * time.Millisecond)
	}
}

type weatherResponse struct {
	Daily struct {
		Time           []string  `json:"time"`
		TemperatureMax []float64 `json:"temperature_2m_max"`
		TemperatureMin []float64 `json:"temperature_2m_min"`
		WeatherCode    []int     `json:"weathercode"`
	} `json:"daily"`
}

type forecast struct {
	MinC float64
	MaxC float64
	Code int
	Date string
}

func buildTomorrowWeatherMessage() string {
	lesna, err := fetchTomorrowForecast(51.0156, 15.2634)
	if err != nil {
		log.Println("weather Lesna error:", err)
		return ""
	}
	bielsko, err := fetchTomorrowForecast(49.8224, 19.0469)
	if err != nil {
		log.Println("weather Bielsko error:", err)
		return ""
	}

	var b strings.Builder
	b.WriteString("🌤️ **Pogoda na jutro**\n")
	b.WriteString(fmt.Sprintf("Leśna: %s, %.0f/%.0f°C\n", weatherDescription(lesna.Code), lesna.MinC, lesna.MaxC))
	b.WriteString(fmt.Sprintf("Bielsko-Biała: %s, %.0f/%.0f°C", weatherDescription(bielsko.Code), bielsko.MinC, bielsko.MaxC))
	return b.String()
}

func fetchTomorrowForecast(lat, lon float64) (forecast, error) {
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode&timezone=Europe/Warsaw&forecast_days=2", lat, lon)
	resp, err := http.Get(url)
	if err != nil {
		return forecast{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return forecast{}, fmt.Errorf("bad status: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return forecast{}, err
	}
	var parsed weatherResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return forecast{}, err
	}
	if len(parsed.Daily.Time) < 2 || len(parsed.Daily.TemperatureMax) < 2 || len(parsed.Daily.TemperatureMin) < 2 || len(parsed.Daily.WeatherCode) < 2 {
		return forecast{}, fmt.Errorf("insufficient forecast data")
	}

	return forecast{
		Date: parsed.Daily.Time[1],
		MaxC: parsed.Daily.TemperatureMax[1],
		MinC: parsed.Daily.TemperatureMin[1],
		Code: parsed.Daily.WeatherCode[1],
	}, nil
}

func weatherDescription(code int) string {
	switch code {
	case 0:
		return "bezchmurnie"
	case 1, 2, 3:
		return "częściowe zachmurzenie"
	case 45, 48:
		return "mgła"
	case 51, 53, 55:
		return "mżawka"
	case 56, 57:
		return "marznąca mżawka"
	case 61, 63, 65:
		return "deszcz"
	case 66, 67:
		return "marznący deszcz"
	case 71, 73, 75:
		return "śnieg"
	case 77:
		return "ziarna śniegu"
	case 80, 81, 82:
		return "przelotne opady"
	case 85, 86:
		return "przelotne opady śniegu"
	case 95:
		return "burza"
	case 96, 99:
		return "burza z gradem"
	default:
		return "pogoda"
	}
}
