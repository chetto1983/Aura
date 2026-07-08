import { postJSON } from '../api/json';

export interface PasswordResetStartRequest {
  readonly email: string;
}

export interface PasswordResetVerifyRequest {
  readonly email: string;
  readonly code: string;
  readonly answer: string;
}

export interface PasswordResetVerifyResponse {
  readonly resetToken: string;
}

export interface PasswordResetQuestionRequest {
  readonly email: string;
  readonly code: string;
}

export interface PasswordResetQuestionResponse {
  readonly question: string;
}

export interface PasswordResetCompleteRequest {
  readonly resetToken: string;
  readonly password: string;
}

export interface PasswordResetStatusResponse {
  readonly status: string;
}

export function startPasswordReset(
  req: PasswordResetStartRequest,
): Promise<PasswordResetStatusResponse> {
  return postJSON<PasswordResetStatusResponse>('/api/auth/password-reset/start', req);
}

export function fetchPasswordResetQuestion(
  req: PasswordResetQuestionRequest,
): Promise<PasswordResetQuestionResponse> {
  return postJSON<PasswordResetQuestionResponse>('/api/auth/password-reset/question', req);
}

export function verifyPasswordReset(
  req: PasswordResetVerifyRequest,
): Promise<PasswordResetVerifyResponse> {
  return postJSON<PasswordResetVerifyResponse>('/api/auth/password-reset/verify', req);
}

export function completePasswordReset(
  req: PasswordResetCompleteRequest,
): Promise<PasswordResetStatusResponse> {
  return postJSON<PasswordResetStatusResponse>('/api/auth/password-reset/complete', req);
}
