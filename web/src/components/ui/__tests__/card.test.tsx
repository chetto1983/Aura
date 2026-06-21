import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '../card';

describe('Card', () => {
  it('composes header/title/description/content/footer on the surface token', () => {
    render(
      <Card>
        <CardHeader>
          <CardTitle>Server</CardTitle>
          <CardDescription>desc</CardDescription>
        </CardHeader>
        <CardContent>body</CardContent>
        <CardFooter>foot</CardFooter>
      </Card>,
    );
    expect(screen.getByText('Server')).toBeTruthy();
    expect(screen.getByText('desc')).toBeTruthy();
    expect(screen.getByText('body')).toBeTruthy();
    expect(screen.getByText('foot')).toBeTruthy();
    expect(screen.getByText('Server').closest('[data-slot="card"]')?.className).toContain(
      'bg-card',
    );
  });

  it('merges custom className via cn', () => {
    render(<Card className="custom-x">c</Card>);
    expect(screen.getByText('c').className).toContain('custom-x');
  });
});
