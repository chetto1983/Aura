package telegram

import (
	"strings"

	"github.com/aura/aura/internal/llm"
)

func userRequestedRawOutput(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"output grezzo",
		"raw output",
		"output raw",
		"mostrami l'output",
		"mostra l'output",
		"stampa l'output",
		"incolla l'output",
		"esegui il comando",
		"lancia il comando",
		"run the command",
		"show me the output",
		"show raw",
		"print the output",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(text, "`")
}

func latestUserText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func runtimeToolAllowedForUserIntent(toolName, userText string) bool {
	if toolName != "execute_code" && toolName != "execute_shell" {
		return true
	}
	if userRequestedRawOutput(userText) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	for _, marker := range []string{
		"diagnostica",
		"diagnostic",
		"debug",
		"verifica",
		"controlla",
		"ispeziona",
		"test",
		"build",
		"compila",
		"installa",
		"pip install",
		"npm install",
		"go test",
		"go build",
		"calcola",
		"compute",
		"crea",
		"genera",
		"scrivi",
		"modifica",
		"leggi il file",
		"container",
		"runtime",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func runtimeToolBlockedResult(toolName string) string {
	return "Richiesta conversazionale: non usare " + toolName + ". Rispondi in modo naturale con il contesto gia disponibile, senza ispezionare runtime, filesystem o container."
}
