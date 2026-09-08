import { createContext, useContext } from 'react';
import type { RendererProps } from './renderers/PreviewStatus';

export type OpenArtifact = (artifact: RendererProps) => void;
export const ArtifactWorkspaceContext = createContext<OpenArtifact | null>(null);
export const useArtifactWorkspace = () => useContext(ArtifactWorkspaceContext);
