import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  AppShellSkeleton,
  LoginPageSkeleton,
  NotFoundViewSkeleton,
  RouteSkeletonFallback,
  RuntimeHealthPanelSkeleton,
  SkeletonAvatar,
  SkeletonBlock,
  SkeletonButton,
  SkeletonCard,
  SkeletonChart,
  SkeletonForm,
  SkeletonPage,
  SkeletonTable,
  SkeletonText,
} from '../components/skeleton';
import i18n from '../i18n/i18n';

describe('skeleton primitives', () => {
  it('renders blocks, text, avatar, and button placeholders as non-semantic visuals', () => {
    render(
      <div>
        <SkeletonBlock height={12} radius="lg" testId="block" width={120} />
        <SkeletonText lines={3} testId="text" widths={['80%', '60%', '40%']} />
        <SkeletonAvatar size="lg" testId="avatar" />
        <SkeletonButton testId="button" width="8rem" />
      </div>,
    );

    const block = screen.getByTestId('block');
    expect(block.getAttribute('aria-hidden')).toBe('true');
    expect(block.getAttribute('data-rounded')).toBe('lg');
    expect(block.style.width).toBe('120px');

    expect(screen.getByTestId('text').querySelectorAll('.skeleton-block')).toHaveLength(3);
    expect(screen.getByTestId('avatar').getAttribute('data-rounded')).toBe('full');
    expect(screen.getByTestId('button').style.width).toBe('8rem');
  });

  it('renders card, table, chart, and form skeleton compositions', () => {
    render(
      <div>
        <SkeletonCard testId="card" />
        <SkeletonTable columns={3} rows={2} testId="table" />
        <SkeletonTable mode="scroll" rows={1} showHeader={false} testId="scroll-table" />
        <SkeletonChart height="10rem" legendItems={2} testId="chart" />
        <SkeletonForm fields={2} showActions={false} testId="form" />
      </div>,
    );

    expect(screen.getByTestId('card').querySelectorAll('.skeleton-block').length).toBeGreaterThan(
      1,
    );
    expect(screen.getByTestId('table').querySelectorAll('.skeleton-card')).toHaveLength(3);
    expect(screen.getByTestId('scroll-table').innerHTML).toContain('min-w-[38rem]');
    expect(screen.getByTestId('chart').querySelector('.skeleton-chart-plot')).toBeTruthy();
    expect(screen.getByTestId('form').querySelectorAll('.skeleton-block').length).toBeGreaterThan(
      2,
    );
  });

  it('uses a single optional live region at the page wrapper', () => {
    render(
      <>
        <SkeletonPage label="Loading audit view" testId="page">
          <SkeletonText />
        </SkeletonPage>
        <SkeletonPage announce={false} label="Quiet panel" testId="quiet-page">
          <SkeletonText />
        </SkeletonPage>
      </>,
    );

    expect(screen.getByTestId('page').getAttribute('role')).toBe('status');
    expect(screen.getByTestId('page').getAttribute('aria-busy')).toBe('true');
    expect(screen.getByText('Loading audit view')).toBeTruthy();
    expect(screen.getByTestId('quiet-page').getAttribute('role')).toBe(null);
  });
});

describe('app skeletons', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en');
  });

  afterEach(() => {
    window.history.pushState({}, '', '/');
  });

  it('renders route-level skeletons with the expected accessibility labels', () => {
    render(
      <div>
        <AppShellSkeleton label="Loading shell" />
        <LoginPageSkeleton label="Loading login" />
        <NotFoundViewSkeleton label="Loading not found" />
      </div>,
    );

    expect(screen.getByText('Loading shell')).toBeTruthy();
    expect(screen.getByText('Loading login')).toBeTruthy();
    expect(screen.getByText('Loading not found')).toBeTruthy();
  });

  it('renders a quiet runtime panel skeleton when nested inside another loading region', () => {
    render(<RuntimeHealthPanelSkeleton announce={false} label="Checking runtime" />);

    expect(document.querySelectorAll('.skeleton-block').length).toBeGreaterThan(1);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('selects the route fallback shape from the current path', () => {
    window.history.pushState({}, '', '/login');
    const login = render(<RouteSkeletonFallback />);
    expect(screen.getByText('Loading sign-in...')).toBeTruthy();
    login.unmount();

    window.history.pushState({}, '', '/');
    const shell = render(<RouteSkeletonFallback />);
    expect(screen.getByText('Loading cockpit...')).toBeTruthy();
    shell.unmount();

    window.history.pushState({}, '', '/missing');
    render(<RouteSkeletonFallback />);
    expect(screen.getByText('Loading page...')).toBeTruthy();
  });

  it('updates a visible route fallback when the language changes', async () => {
    window.history.pushState({}, '', '/login');
    render(<RouteSkeletonFallback />);
    expect(screen.getByText('Loading sign-in...')).toBeTruthy();

    await i18n.changeLanguage('it');
    expect(screen.getByText('Caricamento accesso...')).toBeTruthy();
  });
});
