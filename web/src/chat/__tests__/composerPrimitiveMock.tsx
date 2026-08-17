import {
  cloneElement,
  isValidElement,
  type ButtonHTMLAttributes,
  type HTMLAttributes,
  type ReactElement,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';

// ONE double for ComposerPrimitive, shared by every composer test.
//
// Three files mocked it independently and the copies drifted: a primitive adopted in the
// component had to be added to each of them, and the one that was missed failed with
// "type is invalid" rather than with the assertion the test was about. The behaviour is the
// library's and is not re-implemented here — each part is stubbed to its MOUNT, plus the one
// call the component's own logic is written against (Dictate → startDictation).

export interface ComposerPrimitiveMockHandle {
  // Optional because a test that never renders the mic (the draft-prompt one) has no reason
  // to carry two spies for it; the stubs fall back to a no-op rather than making every
  // handle declare a dictation surface it does not exercise.
  readonly startDictation?: (() => void) | undefined;
  readonly stopDictation?: (() => void) | undefined;
  /** Mirrors useComposerDictate().disabled inverted: false ⇒ no DictationAdapter is
   * configured. Read at call time so a test can flip it between renders. */
  dictateAvailable?: boolean;
}

/** coreReact doubles @assistant-ui/core/react, which the Composer reads for ONE thing: whether
 * dictation is actually available. Mocking it is what lets a test drive the D-10 fallback from
 * the same signal production uses, instead of from a caps flag that only stands in for it. */
export function coreReactMock(h: ComposerPrimitiveMockHandle) {
  return {
    useComposerDictate: () => ({
      startDictation: h.startDictation ?? noop,
      disabled: h.dictateAvailable === false,
    }),
  };
}

interface RenderProp {
  render?: ReactNode;
}

/** withAction clones a `render` element so the stub performs the primitive's action AND the
 * component's own onClick, which is what the real createActionButton composes. */
const noop = () => undefined;

function withAction(render: ReactNode, action: () => void): ReactNode {
  if (!isValidElement(render)) return render;
  const element = render as ReactElement<{ onClick?: () => void }>;
  return cloneElement(element, {
    onClick: () => {
      action();
      element.props.onClick?.();
    },
  });
}

/** spread is what a vi.mock factory spreads in: `...(await import(…)).spread(h)`. It exists so
 * the mocked module's ComposerPrimitive key is named in ONE place rather than in each factory. */
export function spread(h: ComposerPrimitiveMockHandle) {
  return { ComposerPrimitive: composerPrimitiveMock(h) };
}

export function composerPrimitiveMock(h: ComposerPrimitiveMockHandle) {
  return {
    // Root is the form in the real library; a form here too, so a submit-driven test is
    // exercising the same element kind the browser gets.
    Root: (props: HTMLAttributes<HTMLFormElement>) => <form {...props} />,
    AttachmentDropzone: ({
      children,
      disabled,
      ...props
    }: HTMLAttributes<HTMLDivElement> & { disabled?: boolean }) => (
      <div data-disabled={disabled === true ? 'true' : undefined} {...props}>
        {children}
      </div>
    ),
    Input: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
    Cancel: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="button" {...props} />,
    Send: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="submit" {...props} />,
    AddAttachment: (props: ButtonHTMLAttributes<HTMLButtonElement> & RenderProp) =>
      props.render ?? <button type="button" {...props} />,
    Dictate: ({ render }: RenderProp) => <>{withAction(render, h.startDictation ?? noop)}</>,
    StopDictation: ({ render }: RenderProp) => <>{withAction(render, h.stopDictation ?? noop)}</>,
    Attachments: ({ children }: { children?: (v: { attachment: unknown }) => ReactNode }) =>
      children ? null : null,
    // The trigger popover is stubbed to its MOUNT, not its behaviour: it records the char it
    // was registered for and swallows the rest. Driving the real popover here would be
    // testing the library through a mock of the library.
    Unstable_TriggerPopoverRoot: ({ children }: { children: ReactNode }) => <>{children}</>,
    Unstable_TriggerPopover: Object.assign(
      ({ char }: { char: string }) => <div data-testid="trigger-popover" data-char={char} />,
      { Directive: () => null, Action: () => null },
    ),
    Unstable_TriggerPopoverItems: () => null,
    Unstable_TriggerPopoverItem: () => null,
  };
}
