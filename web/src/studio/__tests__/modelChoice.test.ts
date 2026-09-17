import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { StudioModel } from '../studioApi';
import { initialModel, rememberModel, rememberedModel } from '../modelChoice';

function model(id: string): StudioModel {
  return { id, audio: false, seed: false };
}

const listed = [model('veo'), model('sora'), model('kling')];

describe('the remembered model', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('is kept per kind, under the key the Studio owns', () => {
    rememberModel('video', 'veo');
    rememberModel('image', 'seedream');

    expect(localStorage.getItem('aura.studio.model.video')).toBe('veo');
    expect(localStorage.getItem('aura.studio.model.image')).toBe('seedream');
    // One key per kind: picking a video model must not change which image model opens.
    expect(rememberedModel('video')).toBe('veo');
    expect(rememberedModel('image')).toBe('seedream');
  });

  it('is nothing before anything has been picked', () => {
    expect(rememberedModel('video')).toBeUndefined();
  });

  it('opens on the remembered model while the deployment still lists it', () => {
    rememberModel('video', 'kling');

    expect(initialModel('video', listed, 'veo')).toBe('kling');
  });

  it('falls back to the deployment default when the remembered model is gone', () => {
    rememberModel('video', 'a-model-the-catalog-dropped');

    expect(initialModel('video', listed, 'sora')).toBe('sora');
  });

  it('falls back to the first listed model when the deployment default is not listed either', () => {
    rememberModel('video', 'gone');

    expect(initialModel('video', listed, 'also-gone')).toBe('veo');
  });

  it('is empty when the deployment lists no model at all', () => {
    expect(initialModel('video', [], 'veo')).toBe('');
  });

  it('survives a browser that refuses storage', () => {
    // A private window throws on both accessors. The picker still has to open.
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage is denied');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage is denied');
    });

    expect(() => {
      rememberModel('video', 'veo');
    }).not.toThrow();
    expect(rememberedModel('video')).toBeUndefined();
    expect(initialModel('video', listed, 'sora')).toBe('sora');
  });
});
