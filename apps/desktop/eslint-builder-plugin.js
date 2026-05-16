const componentDirectories = new Set(["components", "ui"]);
const dtoImportNames = new Set(["Dto", "DTO"]);
const rawProtocolDirectories = new Set(["generated", "protocol"]);
const disallowedEffectCalls = new Set(["fetch", "invoke"]);
const knownBridgeIdentifiers = new Set(["apiClient", "builderClient", "nativeBridge", "serverClient"]);

export const builderArchitecture = {
  rules: {
    "no-array-index-key": {
      meta: {
        type: "problem",
        docs: {
          description: "Disallow unstable array index values as React keys.",
        },
        messages: {
          indexKey: "Do not use array indexes as React keys. Use a stable domain identifier.",
        },
        schema: [],
      },
      create(context) {
        return {
          JSXAttribute(node) {
            if (node.name.name !== "key" || node.value?.type !== "JSXExpressionContainer") {
              return;
            }

            if (isIndexLikeExpression(node.value.expression)) {
              context.report({ node, messageId: "indexKey" });
            }
          },
        };
      },
    },
    "no-mutable-exports": {
      meta: {
        type: "problem",
        docs: {
          description: "Disallow exported mutable bindings.",
        },
        messages: {
          mutableExport: "Do not export mutable bindings. Export constants, functions, classes, or immutable factories.",
        },
        schema: [],
      },
      create(context) {
        return {
          ExportNamedDeclaration(node) {
            if (node.declaration?.type === "VariableDeclaration" && node.declaration.kind !== "const") {
              context.report({ node, messageId: "mutableExport" });
            }
          },
        };
      },
    },
    "no-raw-dto-in-components": {
      meta: {
        type: "problem",
        docs: {
          description: "Keep generated protocol DTOs out of React component files.",
        },
        messages: {
          rawDto: "Do not import raw protocol DTOs in component files. Map DTOs to view models at feature boundaries.",
        },
        schema: [],
      },
      create(context) {
        const filename = context.filename ?? context.getFilename();
        if (!isComponentFile(filename)) {
          return {};
        }

        return {
          ImportDeclaration(node) {
            if (isRawProtocolImportSource(node.source.value) || hasDtoImportSpecifier(node.specifiers)) {
              context.report({ node, messageId: "rawDto" });
            }
          },
        };
      },
    },
    "no-useeffect-data-loading": {
      meta: {
        type: "problem",
        docs: {
          description: "Disallow ad-hoc data loading and bridge calls inside React component effects.",
        },
        messages: {
          dataLoading:
            "Do not load data or call native/backend bridges directly inside component useEffect. Move the side effect into a service or dedicated hook.",
        },
        schema: [],
      },
      create(context) {
        const filename = context.filename ?? context.getFilename();
        if (!isComponentFile(filename)) {
          return {};
        }

        const reactEffectNames = new Set(["useEffect"]);
        const reactNamespaces = new Set(["React"]);
        const bridgeIdentifiers = new Set(knownBridgeIdentifiers);

        return {
          ImportDeclaration(node) {
            collectReactEffectBindings(node, reactEffectNames, reactNamespaces);
            collectBridgeBindings(node, bridgeIdentifiers);
          },
          CallExpression(node) {
            if (!isUseEffectCall(node, reactEffectNames, reactNamespaces)) {
              return;
            }

            const effect = node.arguments[0];
            if (effect !== undefined && containsDisallowedEffectCall(effect, bridgeIdentifiers)) {
              context.report({ node, messageId: "dataLoading" });
            }
          },
        };
      },
    },
  },
};

function isIndexLikeExpression(expression) {
  if (expression.type === "Identifier") {
    return ["i", "idx", "index"].includes(expression.name);
  }

  if (expression.type === "MemberExpression" && expression.computed) {
    return isIndexLikeExpression(expression.property);
  }

  return false;
}

function isComponentFile(filename) {
  const path = normalizePath(filename);
  const segments = path.split("/");
  const basename = segments.at(-1) ?? "";
  const isTsComponent = basename.endsWith(".tsx") || basename.endsWith(".ts");
  if (!isTsComponent) {
    return false;
  }

  return segments.some((segment) => componentDirectories.has(segment)) || startsWithUppercaseAscii(basename);
}

function normalizePath(path) {
  return path.split("\\").join("/");
}

function startsWithUppercaseAscii(value) {
  const first = value.charCodeAt(0);
  return first >= 65 && first <= 90;
}

function isRawProtocolImportSource(value) {
  if (typeof value !== "string") {
    return false;
  }

  return value.split("/").some(isRawProtocolImportSegment);
}

function isRawProtocolImportSegment(segment) {
  return rawProtocolDirectories.has(segment) || segment === "dto" || segment.endsWith(".dto");
}

function hasDtoImportSpecifier(specifiers) {
  return specifiers.some((specifier) => {
    if (specifier.type !== "ImportSpecifier") {
      return false;
    }

    return isDtoName(getSpecifierName(specifier.imported)) || isDtoName(specifier.local.name);
  });
}

function getSpecifierName(specifier) {
  if (specifier.type === "Identifier") {
    return specifier.name;
  }

  return String(specifier.value);
}

function isDtoName(name) {
  return [...dtoImportNames].some((suffix) => name.endsWith(suffix));
}

function collectReactEffectBindings(node, reactEffectNames, reactNamespaces) {
  if (node.source.value !== "react") {
    return;
  }

  for (const specifier of node.specifiers) {
    if (specifier.type === "ImportNamespaceSpecifier") {
      reactNamespaces.add(specifier.local.name);
      continue;
    }

    if (specifier.type === "ImportSpecifier" && getSpecifierName(specifier.imported) === "useEffect") {
      reactEffectNames.add(specifier.local.name);
    }
  }
}

function collectBridgeBindings(node, bridgeIdentifiers) {
  if (node.source.value !== "@builder/desktop-native-bridge") {
    return;
  }

  for (const specifier of node.specifiers) {
    bridgeIdentifiers.add(specifier.local.name);
  }
}

function isUseEffectCall(node, reactEffectNames, reactNamespaces) {
  if (node.callee.type === "Identifier") {
    return reactEffectNames.has(node.callee.name);
  }

  if (node.callee.type !== "MemberExpression") {
    return false;
  }

  return (
    node.callee.object.type === "Identifier" &&
    reactNamespaces.has(node.callee.object.name) &&
    getMemberPropertyName(node.callee) === "useEffect"
  );
}

function containsDisallowedEffectCall(node, bridgeIdentifiers) {
  let found = false;

  visit(node, new WeakSet(), (child) => {
    if (child.type !== "CallExpression") {
      return;
    }

    if (isDisallowedEffectCallee(child.callee, bridgeIdentifiers)) {
      found = true;
    }
  });

  return found;
}

function isDisallowedEffectCallee(callee, bridgeIdentifiers) {
  if (callee.type === "Identifier") {
    return disallowedEffectCalls.has(callee.name);
  }

  if (callee.type !== "MemberExpression") {
    return false;
  }

  return disallowedEffectCalls.has(getMemberPropertyName(callee)) || isKnownBridgeObject(callee.object, bridgeIdentifiers);
}

function isKnownBridgeObject(expression, bridgeIdentifiers) {
  if (expression.type === "Identifier") {
    return bridgeIdentifiers.has(expression.name);
  }

  if (expression.type === "MemberExpression") {
    return isKnownBridgeObject(expression.object, bridgeIdentifiers);
  }

  return false;
}

function getMemberPropertyName(node) {
  if (node.property.type === "Identifier") {
    return node.property.name;
  }

  if (node.property.type === "Literal") {
    return String(node.property.value);
  }

  return "";
}

function visit(node, seen, callback) {
  if (seen.has(node)) {
    return;
  }
  seen.add(node);
  callback(node);

  for (const [key, value] of Object.entries(node)) {
    if (key === "parent") {
      continue;
    }
    if (value === null || value === undefined) {
      continue;
    }

    if (Array.isArray(value)) {
      for (const child of value) {
        if (isAstNode(child)) {
          visit(child, seen, callback);
        }
      }
      continue;
    }

    if (isAstNode(value)) {
      visit(value, seen, callback);
    }
  }
}

function isAstNode(value) {
  return typeof value === "object" && typeof value.type === "string";
}
