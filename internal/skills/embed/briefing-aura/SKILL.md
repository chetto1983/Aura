---
name: briefing-aura
description: "The operator's daily briefing: today's calendar, the board's overdue and due cards, and the unread mail that asks them for something, read live and written as one short message. Load this BEFORE the first read whenever the goal or the operator asks for the briefing, the day's summary, 'what is on today' or 'buongiorno, cosa ho oggi'. It carries the rules that make the briefing trustworthy: every line comes from a tool result of this run, nothing is changed, and a source that fails is named, never filled in."
metadata:
  short-description: Write the daily briefing from calendar, board and unread mail
---

# The daily briefing

One message the operator reads in under a minute. It is read-only: you look, you never change
anything. Every item in it comes from a tool result of this run; what you did not read, you do
not write.

## Today

"Today" is the date in your clock hint (the current-time line), in that line's zone. Board due
dates are in UTC: convert each one to that zone before you compare it with today.

## The three reads

The tools are deferred: find them first with `tool_search` ("board", "calendar"). Then make the
three reads in parallel, because none depends on another:

1. `board` with `{"action":"list"}`. Its first section lists what is due within 24 hours,
   overdue first; the columns follow.
2. `calendar__calendar` with `{"action":"get_calendar_events","timeZone":"<your clock's zone>",
   "startDate":"<today>","endDate":"<today>"}`.
3. `calendar__calendar` with `{"action":"get_emails","unreadOnly":true,"count":20}`.

When `tool_search` finds no `calendar__calendar`, no mailbox is connected: skip reads 2 and 3
and say so in one line. When a read fails, name it in one line ("calendar unavailable") and go
on with the others. Never retry a failed read more than once.

## What you write, in this order

Omit a section that has nothing in it. Never write an empty heading.

1. **The day**, at most three lines: how full it is and the one thing that matters most.
2. **Events**, in time order: start time, label, and an overlap named when two events overlap.
3. **Cards**: overdue first, then due today, then what sits in the `doing` column. Name a card by
   its label, never by its id. When the board says more cards are not shown, write that, and do
   not guess what they are.
4. **Unread mail**: the count, then at most five messages that ask the operator for something,
   judged from sender and subject only. One line each: sender, subject.

A quiet day is one line, for example "Nothing on the calendar, nothing due, no unread mail."
Keep the whole message under 1,500 characters: short lines, no tables, no ids.

Write in the operator's language: the language of the goal, or of the conversation that
scheduled you.

## Forbidden

- Any write: `board` add, update, move or delete; `calendar__calendar` create_event,
  update_event, respond_to_event, send_email, or anything that marks mail read.
- Opening a message: no `get_email_details`, no `search_emails`. The briefing works from what
  `get_emails` returns.
- Memory, the web, files: the briefing has three sources and no others.
- Scheduling: you are the scheduled run. Do not create, change or cancel a task.
- Inventing: an event, a card or a message you did not read in this run does not exist.
