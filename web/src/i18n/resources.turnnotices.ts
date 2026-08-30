import { chatBudgetEn, chatBudgetIt } from './resources.budget';
import { chatSteerEn, chatSteerIt } from './resources.steer';

// The thread-level notices a live turn can raise — a redirected steer (Phase 52) and a
// budget trip (amendment #188) — grouped so resources.ts spreads one object per locale
// under `chat` and stays under the 600-LOC cap it sits right against.
export const chatTurnNoticesEn = { steer: chatSteerEn, budget: chatBudgetEn };
export const chatTurnNoticesIt = { steer: chatSteerIt, budget: chatBudgetIt };
