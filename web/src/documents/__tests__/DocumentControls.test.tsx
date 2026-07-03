import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { DocumentItem, DocumentVersion } from '../documentApi';
import { DocumentFileList } from '../DocumentFileList';
import { DocumentFilterBar } from '../DocumentFilterBar';
import { DocumentLibraryHeader } from '../DocumentLibraryHeader';
import { DocumentUploadDialog } from '../DocumentUploadDialog';

const uploadLibraryDocument = vi.hoisted(() => vi.fn());

vi.mock('../documentUpload', () => ({
  uploadLibraryDocument,
}));

const baseDocument: DocumentItem = {
  id: 'doc-1',
  identity_id: 'identity-1',
  scope: 'library',
  title: 'Robot Guide.pdf',
  tags: ['robot', 'manual', 'training'],
  metadata: {},
  active_version_id: 'ver-1',
  status: 'ready',
  created_at: '2026-06-18T10:00:00Z',
  updated_at: '2026-06-19T10:00:00Z',
};

const baseVersion: DocumentVersion = {
  id: 'ver-1',
  document_id: 'doc-1',
  asset_id: 'asset-1',
  version_number: 1,
  status: 'ready',
  sha256: 'sha256-1',
  content_type: 'application/pdf',
  size_bytes: 4096,
  storage_object_id: 'storage-1',
  created_at: '2026-06-19T10:00:00Z',
};

function renderFileList(overrides: Partial<ComponentProps<typeof DocumentFileList>> = {}) {
  const props: ComponentProps<typeof DocumentFileList> = {
    documents: [baseDocument],
    activeVersions: new Map([[baseDocument.id, baseVersion]]),
    tab: 'all',
    viewMode: 'list',
    selectedIds: new Set(),
    activeId: '',
    loading: false,
    error: false,
    onToggleSelected: vi.fn(),
    onOpenDetails: vi.fn(),
    onOpenActions: vi.fn(),
    onAskDocument: vi.fn(),
    onEditTags: vi.fn(),
    onRetry: vi.fn(),
    onDelete: vi.fn(),
    onRefresh: vi.fn(),
    ...overrides,
  };
  render(<DocumentFileList {...props} />);
  return props;
}

function openActions(button: HTMLElement) {
  fireEvent.pointerDown(button, { button: 0, ctrlKey: false });
}

describe('Document library controls', () => {
  afterEach(() => {
    uploadLibraryDocument.mockReset();
  });

  it('keeps mobile search as the 44px input instead of a hidden 1px submit target', () => {
    const onSearch = vi.fn();

    const { container } = render(
      <DocumentLibraryHeader
        query=""
        refreshing={false}
        onQueryChange={vi.fn()}
        onSearch={onSearch}
        onRefresh={vi.fn()}
        onUpload={vi.fn()}
      />,
    );

    const mobileForm = container.querySelector('form.sm\\:hidden');
    expect(mobileForm).not.toBeNull();
    expect(mobileForm?.querySelector('button[type="submit"]')).toBeNull();
    expect(screen.getAllByRole('searchbox', { name: 'Search documents' })[0]?.className).toContain(
      'min-h-[44px]',
    );

    if (mobileForm === null) throw new Error('mobile search form missing');
    fireEvent.submit(mobileForm);
    expect(onSearch).toHaveBeenCalledTimes(1);
  });

  it('routes filter, scope, and view changes through explicit controls', () => {
    const onTabChange = vi.fn();
    const onTagChange = vi.fn();
    const onScopeChange = vi.fn();
    const onViewModeChange = vi.fn();

    render(
      <DocumentFilterBar
        tab="all"
        tag=""
        scope="all"
        viewMode="list"
        onTabChange={onTabChange}
        onTagChange={onTagChange}
        onScopeChange={onScopeChange}
        onViewModeChange={onViewModeChange}
      />,
    );

    expect(screen.getByRole('tab', { name: 'Failed' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Processing' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Tag filter'), { target: { value: 'robot' } });
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'thread' } });
    fireEvent.click(screen.getByRole('button', { name: 'Grid view' }));
    fireEvent.click(screen.getByRole('button', { name: 'List view' }));

    expect(onTabChange).not.toHaveBeenCalled();
    expect(onTagChange).toHaveBeenCalledWith('robot');
    expect(onScopeChange).toHaveBeenCalledWith('thread');
    expect(onViewModeChange).toHaveBeenCalledWith('grid');
    expect(onViewModeChange).toHaveBeenCalledWith('list');
  });

  it('renders loading, error, and empty list states with refresh recovery', () => {
    const onRefresh = vi.fn();

    const { rerender } = render(
      <DocumentFileList
        documents={[]}
        activeVersions={new Map()}
        tab="all"
        viewMode="list"
        selectedIds={new Set()}
        activeId=""
        loading
        error={false}
        onToggleSelected={vi.fn()}
        onOpenDetails={vi.fn()}
        onOpenActions={vi.fn()}
        onAskDocument={vi.fn()}
        onEditTags={vi.fn()}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByRole('status')).toBeTruthy();

    rerender(
      <DocumentFileList
        documents={[]}
        activeVersions={new Map()}
        tab="all"
        viewMode="list"
        selectedIds={new Set()}
        activeId=""
        loading={false}
        error
        onToggleSelected={vi.fn()}
        onOpenDetails={vi.fn()}
        onOpenActions={vi.fn()}
        onAskDocument={vi.fn()}
        onEditTags={vi.fn()}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByRole('alert').textContent).toContain("Couldn't load documents.");
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(onRefresh).toHaveBeenCalledTimes(1);

    rerender(
      <DocumentFileList
        documents={[]}
        activeVersions={new Map()}
        tab="all"
        viewMode="list"
        selectedIds={new Set()}
        activeId=""
        loading={false}
        error={false}
        onToggleSelected={vi.fn()}
        onOpenDetails={vi.fn()}
        onOpenActions={vi.fn()}
        onAskDocument={vi.fn()}
        onEditTags={vi.fn()}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByText('No documents')).toBeTruthy();
  });

  it('filters visible rows by tab and preserves row actions', () => {
    const imageDocument: DocumentItem = {
      ...baseDocument,
      id: 'doc-image',
      title: 'Robot Photo.png',
      tags: ['image'],
      active_version_id: 'ver-image',
    };
    const imageVersion: DocumentVersion = {
      ...baseVersion,
      id: 'ver-image',
      document_id: 'doc-image',
      content_type: 'image/png',
      size_bytes: 512,
    };
    const onToggleSelected = vi.fn();
    const onOpenDetails = vi.fn();
    const onOpenActions = vi.fn();

    renderFileList({
      documents: [baseDocument, imageDocument],
      activeVersions: new Map([
        [baseDocument.id, undefined],
        [imageDocument.id, imageVersion],
      ]),
      tab: 'images',
      selectedIds: new Set(['doc-image']),
      activeId: 'doc-image',
      onToggleSelected,
      onOpenDetails,
      onOpenActions,
    });

    expect(screen.queryByRole('button', { name: /^Robot Guide\.pdf/ })).toBeNull();
    const row = screen.getByRole('row', { name: /Robot Photo\.png.*ready.*512 B/i });
    expect(row.getAttribute('aria-selected')).toBe('true');
    expect(
      screen
        .getAllByRole('button', { name: /^Robot Photo\.png/ })
        .some((button) => button.className.includes('min-h-[44px]')),
    ).toBe(true);

    fireEvent.click(within(row).getByRole('checkbox', { name: 'Select Robot Photo.png' }));
    fireEvent.click(within(row).getByRole('button', { name: /^Robot Photo\.png/ }));
    openActions(within(row).getByRole('button', { name: 'Actions for Robot Photo.png' }));

    expect(onToggleSelected).toHaveBeenCalledWith('doc-image');
    expect(onOpenDetails).toHaveBeenCalledWith('doc-image');
    expect(onOpenActions).toHaveBeenCalledWith('doc-image');
  });

  it('shows the denormalized list-row size when no detail version is loaded', () => {
    renderFileList({
      documents: [
        { ...baseDocument, active_size_bytes: 18022, active_content_type: 'application/pdf' },
      ],
      activeVersions: new Map([[baseDocument.id, undefined]]),
    });

    const row = screen.getByRole('row', { name: /Robot Guide\.pdf/ });
    expect(row.textContent).toContain('17.6 KB');
    expect(row.textContent).not.toContain('-');
  });

  it('renders a card grid when the grid view is selected', () => {
    const onOpenDetails = vi.fn();
    const onOpenActions = vi.fn();

    renderFileList({
      documents: [{ ...baseDocument, active_size_bytes: 18022 }],
      activeVersions: new Map([[baseDocument.id, undefined]]),
      viewMode: 'grid',
      onOpenDetails,
      onOpenActions,
    });

    expect(screen.queryByRole('table')).toBeNull();
    const card = screen.getByRole('listitem');
    expect(card.textContent).toContain('Robot Guide.pdf');
    expect(card.textContent).toContain('17.6 KB');

    fireEvent.click(within(card).getByRole('button', { name: /^Robot Guide\.pdf/ }));
    openActions(within(card).getByRole('button', { name: 'Actions for Robot Guide.pdf' }));
    expect(onOpenDetails).toHaveBeenCalledWith('doc-1');
    expect(onOpenActions).toHaveBeenCalledWith('doc-1');
  });

  it('keeps upload disabled until a file exists and reports success progress', async () => {
    const onOpenChange = vi.fn();
    const onUploaded = vi.fn();
    let finishUpload: (() => void) | undefined;
    uploadLibraryDocument.mockImplementation(
      async (_file: File, onProgress: (value: number) => void) => {
        onProgress(0.42);
        await new Promise<void>((resolve) => {
          finishUpload = resolve;
        });
      },
    );

    render(<DocumentUploadDialog open onOpenChange={onOpenChange} onUploaded={onUploaded} />);

    const upload = screen.getByRole('button', { name: 'Upload' });
    expect((upload as HTMLButtonElement).disabled).toBe(true);

    const file = new File(['robot'], 'robot.pdf', { type: 'application/pdf' });
    fireEvent.change(screen.getByLabelText('Document file'), { target: { files: [file] } });
    fireEvent.click(upload);

    expect((await screen.findByRole('status')).textContent).toContain('Uploading 42%');
    finishUpload?.();
    await waitFor(() => {
      expect(uploadLibraryDocument).toHaveBeenCalledWith(file, expect.any(Function));
      expect(onOpenChange).toHaveBeenCalledWith(false);
      expect(onUploaded).toHaveBeenCalledTimes(1);
    });
  });

  it('surfaces upload failures and lets cancel close the dialog', async () => {
    const onOpenChange = vi.fn();
    uploadLibraryDocument.mockRejectedValue(new Error('virus scan failed'));

    render(<DocumentUploadDialog open onOpenChange={onOpenChange} onUploaded={vi.fn()} />);

    const file = new File(['bad'], 'bad.pdf', { type: 'application/pdf' });
    fireEvent.change(screen.getByLabelText('Document file'), { target: { files: [file] } });
    fireEvent.click(screen.getByRole('button', { name: 'Upload' }));

    expect((await screen.findByRole('alert')).textContent).toContain('virus scan failed');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
