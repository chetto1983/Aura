#!/usr/bin/env python3
"""Browser smoke check for the exact generated HTML, before it becomes deliverable."""
import argparse
import json
import os
from pathlib import Path
from urllib.parse import urlsplit

from playwright.sync_api import sync_playwright


def approved_origins():
    origins = set()
    for raw in os.environ.get('AURA_ARTIFACT_CONNECT_ORIGINS', '').split(','):
        value = raw.strip()
        if not value:
            continue
        parsed = urlsplit(value)
        if (parsed.scheme != 'https' or not parsed.hostname or parsed.username
                or parsed.password or parsed.path not in ('', '/')
                or parsed.query or parsed.fragment or '*' in value):
            raise ValueError('Expected an exact HTTPS API origin: ' + value)
        origins.add('https://' + parsed.netloc)
    return origins


def verify(path, output):
    allowed = approved_origins()
    output.mkdir(parents=True, exist_ok=True)
    results = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            for name, width, height in [('desktop', 1280, 900), ('mobile', 390, 844)]:
                context = browser.new_context(viewport={'width': width, 'height': height},
                                              service_workers='block')
                page = context.new_page()
                errors = []

                def route_request(route):
                    request = route.request
                    url = urlsplit(request.url)
                    if url.scheme not in ('http', 'https'):
                        route.continue_()
                    elif (request.resource_type in ('fetch', 'xhr')
                          and url.scheme + '://' + url.netloc in allowed):
                        route.continue_()
                    else:
                        errors.append('External resource is not bundled/approved: ' + request.url)
                        route.abort()

                page.route('**/*', route_request)
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.on('console', lambda message: errors.append(message.text)
                        if message.type == 'error' else None)
                page.on('requestfailed', lambda request: errors.append(
                    request.failure + ': ' + request.url))
                page.on('response', lambda response: errors.append(
                    str(response.status) + ': ' + response.url) if response.status >= 400 else None)
                try:
                    page.goto(path.as_uri(), wait_until='networkidle', timeout=20000)
                    if not errors:
                        page.locator('body').wait_for(state='visible', timeout=5000)
                        if not page.locator('body').inner_text().strip() and page.locator('canvas,svg').count() == 0:
                            errors.append('The document rendered no visible text, canvas or SVG.')
                        if page.evaluate('document.documentElement.scrollWidth > innerWidth'):
                            errors.append('The document overflows the viewport horizontally.')
                    page.screenshot(path=str(output / (name + '.png')), full_page=True)
                except Exception as error:
                    errors.append(str(error))
                finally:
                    context.close()
                results.append({'viewport': name, 'errors': errors})
        finally:
            browser.close()
    (output / 'report.json').write_text(json.dumps(results, indent=2), encoding='utf-8')
    print(json.dumps(results, indent=2))
    return all(not result['errors'] for result in results)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('html', type=Path)
    parser.add_argument('--output', type=Path, default=Path('artifact-validation'))
    args = parser.parse_args()
    raise SystemExit(0 if verify(args.html.resolve(strict=True), args.output) else 1)
