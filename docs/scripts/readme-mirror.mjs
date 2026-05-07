import path from 'node:path';

import { fromHtml } from 'hast-util-from-html';
import { toHtml } from 'hast-util-to-html';
import { unified } from 'unified';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import remarkStringify from 'remark-stringify';
import { visit } from 'unist-util-visit';

function isFragmentOnly(url) {
  return url.startsWith('#') || url.startsWith('?');
}

function isAbsoluteUrl(url) {
  try {
    return Boolean(new URL(url).protocol);
  } catch {
    return false;
  }
}

function shouldRewriteUrl(url) {
  if (typeof url !== 'string' || url.length === 0) {
    return false;
  }

  if (isFragmentOnly(url) || url.startsWith('/')) {
    return false;
  }

  return !isAbsoluteUrl(url);
}

function splitHash(url) {
  const hashIndex = url.indexOf('#');
  if (hashIndex === -1) {
    return { pathname: url, hash: '' };
  }

  return {
    pathname: url.slice(0, hashIndex),
    hash: url.slice(hashIndex),
  };
}

function getMirroredRootDocumentPath(pathname, docsConfig) {
  const mirroredPaths = new Map([
    ['README.md', docsConfig.docsHomePath],
    ['CONTRIBUTING.md', docsConfig.contributingPath],
    ['SECURITY.md', docsConfig.securityPath],
  ]);

  return mirroredPaths.get(pathname);
}

function rewriteRelativeUrl(url, docsConfig) {
  const { pathname, hash } = splitHash(url);
  const normalizedPath = path.posix.normalize(pathname);
  const mirroredRootDocumentPath = getMirroredRootDocumentPath(normalizedPath, docsConfig);

  if (mirroredRootDocumentPath) {
    return `${mirroredRootDocumentPath}${hash}`;
  }

  const isDirectory = pathname.endsWith('/');
  const extension = path.posix.extname(normalizedPath).toLowerCase();
  const isImage = ['.avif', '.gif', '.jpeg', '.jpg', '.png', '.svg', '.webp'].includes(extension);

  if (isImage) {
    return new URL(normalizedPath, docsConfig.repoRawRootUrl).toString() + hash;
  }

  const targetRoot = isDirectory
    ? `${docsConfig.repoUrl}/tree/${docsConfig.repoDefaultBranch}/`
    : docsConfig.repoBlobRootUrl;
  return new URL(normalizedPath, `${targetRoot}`).toString() + hash;
}

function rewriteHtmlUrlProperties(html, docsConfig) {
  const tree = fromHtml(html, { fragment: true });

  visit(tree, 'element', (node) => {
    const properties = node.properties ?? {};
    for (const propertyName of ['href', 'src', 'poster']) {
      const propertyValue = properties[propertyName];
      if (typeof propertyValue === 'string' && shouldRewriteUrl(propertyValue)) {
        properties[propertyName] = rewriteRelativeUrl(propertyValue, docsConfig);
      }
    }
  });

  return toHtml(tree);
}

function buildFrontmatter(title, editUrl) {
  return [
    '---',
    `title: ${title}`,
    `editUrl: ${editUrl}`,
    '---',
    '',
  ].join('\n');
}

export function mirrorRepoMarkdownDocument(markdown, docsConfig, options) {
  const { title, editPath } = options;
  const processor = unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(() => (tree) => {
      const firstTopLevelHeadingIndex = tree.children.findIndex(
        (node) => node.type === 'heading' && node.depth === 1,
      );

      if (firstTopLevelHeadingIndex >= 0) {
        tree.children.splice(firstTopLevelHeadingIndex, 1);
      }

      visit(tree, (node) => {
        if ((node.type === 'link' || node.type === 'image') && shouldRewriteUrl(node.url)) {
          node.url = rewriteRelativeUrl(node.url, docsConfig);
        }
        if (node.type === 'html') {
          node.value = rewriteHtmlUrlProperties(node.value, docsConfig);
        }
      });
    })
    .use(remarkGfm)
    .use(remarkStringify, {
      bullet: '-',
      fences: true,
      listItemIndent: 'one',
      rule: '-',
      strong: '*',
    });

  const transformedBody = String(processor.processSync(markdown)).trim();
  const frontmatter = buildFrontmatter(title, `${docsConfig.repoEditRootUrl}${editPath}`);

  return `${frontmatter}${transformedBody}\n`;
}

export function mirrorReadme(markdown, docsConfig) {
  return mirrorRepoMarkdownDocument(markdown, docsConfig, {
    title: docsConfig.docsHomeTitle,
    editPath: 'README.md',
  });
}
