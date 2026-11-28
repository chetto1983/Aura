// The routing buttons of the model settings pane and what a save that holds the route says, and
// the Claude route's house key: one Claude Console key the admin sets for every identity, sealed
// and never shown again.

export const providerEn = {
  label: 'Primary model provider',
  cloud: 'Cloud',
  local: 'Local',
  ollama: 'Ollama',
  chatgpt: 'ChatGPT',
  claude: 'Claude',
  routeHeld:
    'Saved, except the model route: once the account lists the model you pick, save again.',
};

export const providerIt = {
  label: 'Provider modello primario',
  cloud: 'Cloud',
  local: 'Locale',
  ollama: 'Ollama',
  chatgpt: 'ChatGPT',
  claude: 'Claude',
  routeHeld:
    'Salvato, tranne il percorso del modello: quando l’account elenca il modello che scegli, salva di nuovo.',
};

export const claudeEn = {
  apiKey: 'Claude API key',
  apiKeyHelp:
    "The Claude Console key whose credit every person's chat uses. Save it first: the account's models are listed once it is stored.",
  priceUnknown: 'price not published',
  keySaved:
    "Claude API key saved. Pick a model from the account's list, then save again to switch the chat to Claude.",
};

export const claudeIt = {
  apiKey: 'Chiave API di Claude',
  apiKeyHelp:
    'La chiave della Claude Console su cui pesa la chat di ogni persona. Salvala prima: i modelli dell’account compaiono quando è salvata.',
  priceUnknown: 'prezzo non pubblicato',
  keySaved:
    'Chiave API di Claude salvata. Scegli un modello dall’elenco dell’account, poi salva di nuovo per passare la chat a Claude.',
};
