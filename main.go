package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	backendURL      = "https://api.kumigaoo.moe/swagger"
	backendInterval = 30 * time.Second
	configFile      = "config.json"
)

type Config struct {
	ChannelID string `json:"channel_id"`
}

type ServerState struct {
	sync.RWMutex

	ChannelID string

	MinecraftOnline bool
	BackendOnline   bool

	Players    map[string]bool
	MaxPlayers int
}

var (
	state = ServerState{
		Players: make(map[string]bool),
	}

	config Config

	discord *discordgo.Session
)
func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN no está configurado")
	}

	if err := loadConfig(); err != nil {
		log.Printf("No se pudo cargar config.json: %v", err)
	}

	state.Lock()
	state.ChannelID = config.ChannelID
	state.Unlock()

	var err error

	discord, err = discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal(err)
	}

	discord.AddHandler(onInteraction)
	discord.AddHandler(onMessageCreate)
discord.Identify.Intents = discordgo.IntentsGuilds |
	discordgo.IntentsGuildMessages |
	discordgo.IntentMessageContent
	if err := discord.Open(); err != nil {
		log.Fatal(err)
	}

	log.Println("Discord conectado.")

	if err := registerCommands(); err != nil {
		log.Fatal(err)
	}

	// Todo lo que usa Discord arranca DESPUÉS de conectar
	go startMinecraftAPI() // recibe join/quit/death/chat del plugin
	go monitorBackend()    // GET api.kumigaoo.moe
	go monitorMinecraft()  // TCP nuestro.kumigaoo.moe:25565

	log.Println("Kumigaoo Bot iniciado.")

	select {}
}

// ============================================================
// CONFIG
// ============================================================

func loadConfig() error {
	data, err := os.ReadFile(configFile)

	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	return json.Unmarshal(data, &config)
}

func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")

	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0600)
}

// ============================================================
// DISCORD COMMANDS
// ============================================================

func registerCommands() error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "setchannel",
			Description: "Configura este canal para los avisos.",
			DefaultMemberPermissions: func() *int64 {
				var permissions int64 = discordgo.PermissionManageGuild
				return &permissions
			}(),
		},
		{
			Name:        "status",
			Description: "Muestra el estado de Minecraft y del backend.",
		},
	}

	for _, guild := range discord.State.Guilds {
		for _, command := range commands {
			_, err := discord.ApplicationCommandCreate(
				discord.State.User.ID,
				guild.ID,
				command,
			)

			if err != nil {
				return fmt.Errorf(
					"no se pudo registrar /%s en %s: %w",
					command.Name,
					guild.Name,
					err,
				)
			}
		}
	}

	return nil
}
func startMinecraftAPI() {
	mux := http.NewServeMux()

	mux.HandleFunc("/minecraft/event", minecraftHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Minecraft API escuchando en :8080")

	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func onInteraction(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	switch i.ApplicationCommandData().Name {
	case "setchannel":
		handleSetChannel(s, i)

	case "status":
		handleStatus(s, i)
	}
}

// ============================================================
// /setchannel
// ============================================================

func handleSetChannel(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
) {
	state.Lock()
	state.ChannelID = i.ChannelID
	state.Unlock()

	config.ChannelID = i.ChannelID

	if err := saveConfig(); err != nil {
		log.Println("Error guardando configuración:", err)

		respond(
			s,
			i,
			"❌ No pude guardar la configuración.",
		)

		return
	}

	respond(
		s,
		i,
		"✅ Este canal ha sido configurado para los avisos.",
	)
}

// ============================================================
// /status
// ============================================================

func handleStatus(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
) {
	state.RLock()

	minecraftOnline := state.MinecraftOnline
	backendOnline := state.BackendOnline
	playerCount := len(state.Players)
	maxPlayers := state.MaxPlayers

	state.RUnlock()

	minecraft := "🔴 Offline"

	if minecraftOnline {
		minecraft = "🟢 Online"
	}

	backend := "🔴 Offline"

	if backendOnline {
		backend = "🟢 Online"
	}

	message := fmt.Sprintf(
		"**Estado de los servicios**\n\n"+
			"🎮 Minecraft: %s\n"+
			"👥 Jugadores: %d/%d\n"+
			"🌐 Backend: %s",
		minecraft,
		playerCount,
		maxPlayers,
		backend,
	)

	respond(s, i, message)
}

func respond(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	message string,
) {
	err := s.InteractionRespond(
		i.Interaction,
		&discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: message,
			},
		},
	)

	if err != nil {
		log.Println("Error respondiendo a Discord:", err)
	}
}

// ============================================================
// BACKEND
// ============================================================

func monitorBackend() {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	check := func() {
		req, err := http.NewRequest(
			http.MethodGet,
			backendURL,
			nil,
		)

		if err != nil {
			log.Println("Error creando request:", err)
			return
		}

		resp, err := client.Do(req)

		online := false

		if err == nil {
			resp.Body.Close()

			online =
				resp.StatusCode >= 200 &&
					resp.StatusCode < 500
		}

		state.Lock()

		previous := state.BackendOnline
		state.BackendOnline = online

		state.Unlock()

		log.Printf(
			"Backend: %s (HTTP %d)",
			statusText(online),
			statusCode(resp),
		)

		if previous != online {
			if online {
				sendNotification(
					"🟢 **Backend online**\n" +
						"`api.kumigaoo.moe` vuelve a responder correctamente.",
				)
			} else {
				sendNotification(
					"🔴 **Backend offline**\n" +
						"`api.kumigaoo.moe` ha dejado de responder correctamente.",
				)
			}
		}
	}

	check()

	ticker := time.NewTicker(backendInterval)
	defer ticker.Stop()

	for range ticker.C {
		check()
	}
}
func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignora bots (incluido el propio) para evitar bucles
	if m.Author == nil || m.Author.Bot {
		return
	}

	state.RLock()
	channelID := state.ChannelID
	state.RUnlock()

	// Solo el canal configurado con /setchannel
	if channelID == "" || m.ChannelID != channelID {
		return
	}

	name := m.Author.Username
	if m.Author.GlobalName != "" {
		name = m.Author.GlobalName
	}
	if m.Member != nil && m.Member.Nick != "" {
		name = m.Member.Nick
	}

	text := m.ContentWithMentionsReplaced()
	text = strings.ReplaceAll(text, "\n", " ")
	if text == "" && len(m.Attachments) > 0 {
		text = "[archivo adjunto]"
	}
	if text == "" {
		return
	}
	if r := []rune(text); len(r) > 256 {
		text = string(r[:256]) + "…"
	}

	go sendToMinecraft(name, text)
}
func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}

	return resp.StatusCode
}

func statusText(online bool) string {
	if online {
		return "ONLINE"
	}

	return "OFFLINE"
}

// ============================================================
// DISCORD NOTIFICATIONS
// ============================================================

func sendNotification(message string) {
	state.RLock()
	channelID := state.ChannelID
	state.RUnlock()

	if channelID == "" {
		log.Println("No hay canal configurado para notificaciones.")
		return
	}

	_, err := discord.ChannelMessageSend(
		channelID,
		message,
	)

	if err != nil {
		log.Println("Error enviando notificación:", err)
	}
}
