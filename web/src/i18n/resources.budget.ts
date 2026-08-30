// The `chat.budget.*` bundle (amendment #188): what the cockpit says when a turn was
// cut by the agent-loop budget instead of ending on its own. Nested under `chat.budget`
// like resources.steer.ts's `chat.steer`; both locales carry every key (parity gate).

export const chatBudgetEn = {
  maxSteps:
    'Turn stopped at the step limit ({{steps}} steps). Send "continue" to keep going, or raise "Max steps per turn" in Settings.',
  wallclock:
    'Turn stopped at the time limit after {{steps}} steps. Send "continue" to keep going, or raise "Max seconds per turn" in Settings.',
  other:
    'Turn stopped by its budget ({{reason}}) after {{steps}} steps. Send "continue" to keep going.',
};

export const chatBudgetIt = {
  maxSteps:
    'Turno interrotto al limite di passi ({{steps}} passi). Scrivi «continua» per proseguire, oppure alza «Passi massimi per turno» nelle Impostazioni.',
  wallclock:
    'Turno interrotto al limite di tempo dopo {{steps}} passi. Scrivi «continua» per proseguire, oppure alza «Secondi massimi per turno» nelle Impostazioni.',
  other:
    'Turno interrotto dal budget ({{reason}}) dopo {{steps}} passi. Scrivi «continua» per proseguire.',
};
