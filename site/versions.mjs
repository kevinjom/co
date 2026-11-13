/** Project site prefix. GitHub Pages serves this repo at /co/. */
export const projectBase = '/co';

const stableTag = /^v\d+\.\d+\.\d+$/;

/**
 * DOCS_BASE, DOCS_VERSION, and DOCS_VERSIONS are read at build time.
 * The publish script rebuilds older tags with these same names.
 */
export function docsBase() {
	const fromEnv = process.env.DOCS_BASE;
	if (typeof fromEnv === 'string' && fromEnv !== '') {
		return withSlashes(fromEnv);
	}
	return `${projectBase}/`;
}

export function docsVersions() {
	const raw = process.env.DOCS_VERSIONS ?? '';
	return raw
		.split(',')
		.map((version) => version.trim())
		.filter((version) => stableTag.test(version));
}

export function docsVersion() {
	const version = (process.env.DOCS_VERSION ?? '').trim();
	return stableTag.test(version) ? version : '';
}

function withSlashes(value) {
	let base = value.startsWith('/') ? value : `/${value}`;
	if (!base.endsWith('/')) base += '/';
	return base;
}
