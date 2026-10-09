import { createContext } from 'react';

/** What a card needs from the board around it: the status of the scheduled task it names, and
 * the minute it is rendered in, which decides whether its due date has passed. */
export interface BoardCardContextValue {
  readonly taskStatus: (taskId: string) => string | undefined;
  readonly now: Date;
}

export const BoardCardContext = createContext<BoardCardContextValue>({
  taskStatus: () => undefined,
  now: new Date(0),
});
