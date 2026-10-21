import { useCallback, useEffect, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useSurfaceIntent } from './useSurfaceIntent';
import type { SurfaceIntent } from './modes';

export function useRoutedSurface() {
  const location = useLocation();
  const navigate = useNavigate();
  const { surface: intent, setSurface: setIntent } = useSurfaceIntent();
  const inDocuments =
    location.pathname === '/documents' || location.pathname.startsWith('/documents/');
  const restore = useRef(location.pathname === '/' && intent === 'documents');
  useEffect(() => {
    if (!restore.current) return;
    restore.current = false;
    void navigate('/documents', { replace: true });
  }, [navigate]);
  // A blocked router transition must leave the document (and its dirty editor) mounted.
  const surface: SurfaceIntent = inDocuments
    ? 'documents'
    : intent === 'documents'
      ? 'chat'
      : intent;
  const setSurface = useCallback(
    (next: SurfaceIntent) => {
      setIntent(next);
      if (next === 'documents' && !inDocuments) void navigate('/documents');
      else if (next !== 'documents' && inDocuments) void navigate('/');
    },
    [inDocuments, navigate, setIntent],
  );
  return { surface, setSurface };
}
