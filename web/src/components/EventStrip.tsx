/** @ts-nocheck */
interface Props { events: string[] }

export function EventStrip({ events }: Props) {
  if (!events.length) return null;
  return (
    <div className="sacchi-event-strip">
      {events.map((e, i) => {
        const isError = e.startsWith('ERROR') || e.startsWith('NET_ERROR');
        const isTool = e.startsWith('TOOL');
        const modifier = isError ? ' sacchi-event-strip__item--error' : isTool ? ' sacchi-event-strip__item--tool' : '';
        return (
          <span key={i} className={`sacchi-event-strip__item${modifier}`}>
            {e}
          </span>
        );
      })}
    </div>
  );
}

export default EventStrip;
