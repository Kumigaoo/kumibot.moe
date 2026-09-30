package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	minecraftAddress  = "nuestro.kumigaoo.moe:25565"
	minecraftInterval = 30 * time.Second
	minecraftFailures = 1 // comprobaciones fallidas seguidas antes de marcar offline
)

type MinecraftEvent struct {
	Event       string   `json:"event"`
	Player      string   `json:"player"`
	UUID        string   `json:"uuid"`
	Message     string   `json:"message"`
	Players     []string `json:"players"`
	PlayerCount int      `json:"player_count"`
	MaxPlayers  int      `json:"max_players"`
}

func minecraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	expected := "Bearer " + os.Getenv("MINECRAFT_TOKEN")
	if r.Header.Get("Authorization") != expected {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event MinecraftEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	handleMinecraftEvent(event)
	w.WriteHeader(http.StatusNoContent)
}

func handleMinecraftEvent(event MinecraftEvent) {
	switch event.Event {
	case "player_join":
		handleMinecraftJoin(event)
	case "player_quit":
		handleMinecraftQuit(event)
	case "player_death":
		handleMinecraftDeath(event)
	case "chat":
		handleMinecraftChat(event)
	}
}

// ============================================================
// MONITOR TCP
// ============================================================

func checkMinecraft() bool {
	conn, err := net.DialTimeout("tcp", minecraftAddress, 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func monitorMinecraft() {
	// Primera comprobación: fija el estado sin notificar
	initial := checkMinecraft()
	state.Lock()
	state.MinecraftOnline = initial
	state.Unlock()

	failures := 0

	ticker := time.NewTicker(minecraftInterval)
	defer ticker.Stop()

	for range ticker.C {
		online := checkMinecraft()

		if online {
			failures = 0
		} else {
			failures++
			if failures < minecraftFailures {
				continue // aún no lo damos por caído
			}
		}

		state.Lock()
		previous := state.MinecraftOnline
		state.MinecraftOnline = online
		if !online {
			state.Players = make(map[string]bool)
		}
		state.Unlock()

		if online && !previous {
			sendNotification(
				"🟢 **Minecraft online**\n" +
					"El servidor de Minecraft está disponible.",
			)
		}

		if !online && previous {
			sendNotification(
				"🔴 **Minecraft offline**\n" +
					"El servidor de Minecraft ha dejado de responder.",
			)
		}
	}
}

// ============================================================
// EVENTOS DEL PLUGIN
// ============================================================

func handleMinecraftJoin(event MinecraftEvent) {
	state.Lock()
	state.Players[event.Player] = true
	if event.MaxPlayers > 0 {
		state.MaxPlayers = event.MaxPlayers
	}
	state.Unlock()

	sendNotification(
		"🟢 **Jugador conectado**\n" +
			"`" + event.Player + "` ha entrado al servidor.",
	)
}

func handleMinecraftQuit(event MinecraftEvent) {
	state.Lock()
	delete(state.Players, event.Player)
	if event.MaxPlayers > 0 {
		state.MaxPlayers = event.MaxPlayers
	}
	state.Unlock()

	sendNotification(
		"🔴 **Jugador desconectado**\n" +
			"`" + event.Player + "` ha salido del servidor.",
	)
}

func handleMinecraftDeath(event MinecraftEvent) {
	message := event.Message
	if message == "" {
		message = event.Player + " ha muerto."
	}

	sendNotification(
		"💀 **Muerte**\n" +
			"`" + message + "`",
	)
}

func handleMinecraftChat(event MinecraftEvent) {
	sendNotification(
		"💬 **Minecraft**\n" +
			"**" + event.Player + ":** " +
			event.Message,
	)
}