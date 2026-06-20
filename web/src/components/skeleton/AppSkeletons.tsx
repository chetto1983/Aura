import i18n from '../../i18n/i18n';
import {
  SkeletonAvatar,
  SkeletonBlock,
  SkeletonButton,
  SkeletonCard,
  SkeletonForm,
  SkeletonPage,
  SkeletonText,
} from './Skeleton';

interface AnnouncedSkeletonProps {
  announce?: boolean;
  label?: string;
}

interface RuntimeHealthPanelSkeletonProps extends AnnouncedSkeletonProps {
  showTitle?: boolean;
}

export function LanguageSwitcherSkeleton() {
  return (
    <div
      aria-hidden="true"
      className="flex shrink-0 rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5"
    >
      <SkeletonButton className="mx-0.5" size="sm" width="2rem" />
      <SkeletonButton className="mx-0.5" size="sm" width="2rem" />
    </div>
  );
}

export function ThemeSwitcherSkeleton() {
  return (
    <div
      aria-hidden="true"
      className="flex shrink-0 rounded-[var(--radius-md)] border border-border bg-surface-2 p-0.5"
    >
      <SkeletonButton className="mx-0.5" size="sm" width="2rem" />
      <SkeletonButton className="mx-0.5" size="sm" width="2rem" />
    </div>
  );
}

function ShellHeaderSkeleton() {
  return (
    <header className="grid min-h-16 grid-cols-[auto_auto_minmax(0,1fr)_auto] items-center gap-2 border-b border-border bg-surface px-2 py-2 sm:px-3">
      <SkeletonButton className="lg:hidden" size="md" width="2.5rem" />
      <SkeletonAvatar size={40} />
      <SkeletonText className="w-14" size="sm" widths="3.25rem" />
      <nav aria-hidden="true" className="hidden items-center gap-1 text-text-muted md:flex">
        {['3rem', '2.75rem', '3.25rem', '4rem', '3.5rem'].map((width) => (
          <SkeletonBlock key={width} height={28} radius="md" width={width} />
        ))}
      </nav>
      <div className="ml-auto hidden items-center gap-2 sm:flex">
        <ThemeSwitcherSkeleton />
        <LanguageSwitcherSkeleton />
      </div>
    </header>
  );
}

function ShellNavigationSkeleton() {
  return (
    <aside className="hidden min-h-0 flex-col gap-3 border-r border-border bg-surface px-3 py-3 lg:flex">
      <SkeletonText size="xs" widths="7rem" />
      <SkeletonText size="xs" widths="8rem" />
      <SkeletonText size="xs" widths="6.5rem" />
    </aside>
  );
}

function ChatWorkspaceSkeleton() {
  return (
    <section className="min-h-0 overflow-hidden bg-bg p-3 sm:p-4">
      <div className="mx-auto flex h-full max-w-5xl flex-col gap-3">
        <SkeletonCard className="min-h-36">
          <div className="flex items-start gap-3">
            <SkeletonAvatar size="lg" />
            <SkeletonText className="flex-1" lines={3} widths={['58%', '86%', '44%']} />
          </div>
        </SkeletonCard>
        <div className="grid min-h-0 flex-1 gap-3 xl:grid-cols-[minmax(0,1fr)_18rem]">
          <SkeletonCard>
            <SkeletonText lines={4} widths={['84%', '72%', '78%', '48%']} />
            <SkeletonBlock height="8rem" radius="md" />
          </SkeletonCard>
          <SkeletonCard className="hidden xl:flex">
            <SkeletonText lines={2} widths={['64%', '42%']} />
            <SkeletonBlock height="7rem" radius="md" />
          </SkeletonCard>
        </div>
        <SkeletonBlock height={52} radius="md" />
      </div>
    </section>
  );
}

export function RuntimeHealthPanelSkeleton({
  announce = true,
  label = 'Checking runtime...',
  showTitle = true,
}: RuntimeHealthPanelSkeletonProps) {
  return (
    <div
      aria-busy="true"
      aria-live={announce ? 'polite' : undefined}
      className="flex flex-col gap-2"
      role={announce ? 'status' : undefined}
    >
      {announce ? <span className="sr-only">{label}</span> : null}
      {showTitle ? <SkeletonText className="w-24" size="lg" widths="5rem" /> : null}
      <div className="flex flex-col pt-1">
        {Array.from({ length: 6 }).map((_, index) => (
          <div
            key={index}
            className="flex min-h-[var(--row-h)] items-center justify-between gap-4 py-1"
          >
            <SkeletonText className="w-24" size="sm" widths={index < 4 ? '4.75rem' : '5.5rem'} />
            <div className="flex items-center gap-2">
              <SkeletonBlock height={8} radius="sm" width={8} />
              <SkeletonText className="w-20" size="sm" widths={index < 2 ? '3.75rem' : '4.5rem'} />
            </div>
          </div>
        ))}
      </div>
      <SkeletonText className="w-36 pt-1" size="xs" widths="7.5rem" />
    </div>
  );
}

function RuntimeWorkspaceSkeleton() {
  return (
    <aside className="hidden min-h-0 overflow-y-auto border-l border-border bg-surface p-4 lg:block">
      <RuntimeHealthPanelSkeleton announce={false} />
    </aside>
  );
}

function BottomDockSkeleton() {
  return (
    <div className="grid border-t border-border bg-surface">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 px-3 py-2 sm:px-4">
        <SkeletonText size="xs" widths="3rem" />
        <SkeletonText size="xs" widths="4rem" />
        <SkeletonText size="xs" widths="3.25rem" />
        <SkeletonBlock className="min-w-[10rem]" height={6} radius="full" />
      </div>
      <div className="grid grid-cols-3 border-t border-border md:hidden">
        {Array.from({ length: 3 }).map((_, index) => (
          <SkeletonBlock key={index} height={44} radius="sm" />
        ))}
      </div>
    </div>
  );
}

export function AppShellSkeleton({ label = 'Loading cockpit...' }: AnnouncedSkeletonProps) {
  return (
    <SkeletonPage
      className="grid h-[100svh] min-h-0 overflow-hidden bg-bg text-text [grid-template-rows:auto_minmax(0,1fr)_auto]"
      label={label}
    >
      <ShellHeaderSkeleton />
      <main className="grid min-h-0 grid-cols-1 lg:grid-cols-[15rem_minmax(0,1fr)_19rem]">
        <ShellNavigationSkeleton />
        <ChatWorkspaceSkeleton />
        <RuntimeWorkspaceSkeleton />
      </main>
      <BottomDockSkeleton />
    </SkeletonPage>
  );
}

export function LoginPageSkeleton({ label = 'Loading sign-in...' }: AnnouncedSkeletonProps) {
  return (
    <SkeletonPage
      className="grid min-h-dvh place-items-center bg-bg px-4 py-6 text-text"
      label={label}
    >
      <div className="w-full max-w-sm rounded-[var(--radius-lg)] border border-border bg-surface p-4">
        <div className="flex items-center justify-between gap-2 pb-5">
          <ThemeSwitcherSkeleton />
          <LanguageSwitcherSkeleton />
        </div>
        <div className="flex flex-col items-center gap-3 pb-6">
          <SkeletonAvatar size={96} />
          <SkeletonText className="w-20" size="xl" widths="4rem" />
          <SkeletonText className="w-36" size="sm" widths="7rem" />
        </div>
        <SkeletonForm fields={1} />
      </div>
    </SkeletonPage>
  );
}

export function NotFoundViewSkeleton({ label = 'Loading page...' }: AnnouncedSkeletonProps) {
  return (
    <SkeletonPage
      className="grid h-dvh place-items-center bg-bg px-6 text-center text-text"
      label={label}
    >
      <SkeletonCard className="w-full max-w-md">
        <SkeletonText className="mx-auto w-48" size="xl" widths="10rem" />
        <SkeletonText className="mx-auto w-72" lines={2} widths={['16rem', '9rem']} />
      </SkeletonCard>
    </SkeletonPage>
  );
}

export function RouteSkeletonFallback() {
  const path = window.location.pathname;
  if (path === '/login') {
    return <LoginPageSkeleton label={i18n.t('skeleton.login')} />;
  }
  if (path === '/') {
    return <AppShellSkeleton label={i18n.t('skeleton.shell')} />;
  }
  return <NotFoundViewSkeleton label={i18n.t('skeleton.page')} />;
}
