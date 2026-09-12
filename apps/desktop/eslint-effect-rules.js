import ts from "typescript";

const rootExecutors = new Set([
  "runFork",
  "runForkWith",
  "runCallback",
  "runCallbackWith",
  "runPromise",
  "runPromiseWith",
  "runPromiseExit",
  "runPromiseExitWith",
  "runSync",
  "runSyncWith",
  "runSyncExit",
  "runSyncExitWith",
  "forkDetach",
]);

const forbiddenMembers = new Map([
  ["effect/Effect", rootExecutors],
  ["effect/Scope", new Set(["make", "makeUnsafe", "globalScope"])],
  ["effect/ManagedRuntime", new Set(["make"])],
  ["effect/Runtime", new Set(["makeRunMain"])],
  ["effect/unstable/reactivity/AtomRegistry", new Set(["make", "layer", "layerOptions"])],
  ["@effect/atom-react", new Set(["RegistryContext"])],
  ["@effect/atom-react/RegistryContext", new Set(["RegistryContext"])],
]);

const namespaceBarrels = new Set(["effect", "effect/unstable/reactivity"]);

function libraryModule(value) {
  if (typeof value !== "string") return null;
  const parts = value.split("/");
  if (parts[0] === "effect") return value;
  if (parts[0] === "@effect" && parts[1] === "atom-react") return value;
  return null;
}

function effectOrigin(origin) {
  return origin !== null && libraryModule(origin.module) !== null;
}

function member(origin, name) {
  if (origin === null) return null;
  if (origin.path.length === 0 && namespaceBarrels.has(origin.module)) {
    return { module: `${origin.module}/${name}`, path: [] };
  }
  return { ...origin, path: [...origin.path, name] };
}

function staticMember(node) {
  if (!node.computed && node.property.type === "Identifier") return node.property.name;
  if (node.computed && node.property.type === "Literal" && typeof node.property.value === "string")
    return node.property.value;
  return null;
}

function patternPath(pattern, name) {
  if (pattern.type === "Identifier") return pattern.name === name ? [] : null;
  if (pattern.type === "AssignmentPattern") return patternPath(pattern.left, name);
  if (pattern.type !== "ObjectPattern") return null;
  for (const property of pattern.properties) {
    if (property.type !== "Property") continue;
    const nested = patternPath(property.value, name);
    if (nested === null) continue;
    const key = property.computed ? property.key.value : (property.key.name ?? property.key.value);
    return typeof key === "string" ? [key, ...nested] : null;
  }
  return null;
}

function originOf(node, source, seen = new Set()) {
  if (node === null || node === undefined || seen.has(node)) return null;
  seen.add(node);
  if (
    [
      "TSAsExpression",
      "TSSatisfiesExpression",
      "TSNonNullExpression",
      "ChainExpression",
      "AwaitExpression",
    ].includes(node.type)
  ) {
    return originOf(node.expression ?? node.argument, source, seen);
  }
  if (node.type === "ImportExpression") {
    const module = libraryModule(node.source.value);
    return module === null ? null : { module, path: [] };
  }
  if (node.type === "CallExpression" && node.callee.type === "Identifier" && node.callee.name === "require") {
    const module = libraryModule(node.arguments[0]?.value);
    return module === null ? null : { module, path: [] };
  }
  if (node.type === "CallExpression") {
    const callee = originOf(node.callee, source, seen);
    if (["node:module", "module"].includes(callee?.module) && callee.path[0] === "createRequire") {
      return { module: "node:module", path: ["require"] };
    }
    if (callee?.module === "node:module" && callee.path[0] === "require") {
      const module = libraryModule(node.arguments[0]?.value);
      return module === null ? null : { module, path: [] };
    }
  }
  if (node.type === "MemberExpression") {
    const key = staticMember(node);
    return key === null ? null : member(originOf(node.object, source, seen), key);
  }
  if (node.type !== "Identifier") return null;
  let scope = source.getScope(node);
  while (scope !== null) {
    const variable = scope.set.get(node.name);
    if (variable !== undefined) {
      for (const definition of variable.defs) {
        if (definition.type === "ImportBinding") {
          const module = definition.parent.source.value;
          const origin = { module, path: [] };
          return definition.node.type === "ImportSpecifier"
            ? member(origin, definition.node.imported.name ?? definition.node.imported.value)
            : origin;
        }
        if (definition.type === "Variable") {
          const path = patternPath(definition.node.id, node.name);
          let origin = originOf(definition.node.init, source, seen);
          if (path === null) return null;
          for (const key of path) origin = member(origin, key);
          return origin;
        }
      }
      return null;
    }
    scope = scope.upper;
  }
  return null;
}

function isForbidden(origin) {
  if (!effectOrigin(origin)) return false;
  if (origin.module.split("/").includes("internal")) return true;
  return forbiddenMembers.get(origin.module)?.has(origin.path[0]) === true;
}

function namespaceEscapes(node) {
  const parent = node.parent;
  if (parent.type === "TSQualifiedName") return false;
  if (parent.type === "MemberExpression" && parent.object === node) return false;
  if (parent.type === "VariableDeclarator" && parent.init === node) return false;
  if (
    [
      "TSAsExpression",
      "TSSatisfiesExpression",
      "TSNonNullExpression",
      "ChainExpression",
      "AwaitExpression",
    ].includes(parent.type)
  )
    return false;
  return true;
}

export const effectRules = {
  "no-effect-subscriptions": {
    meta: {
      type: "problem",
      schema: [],
      messages: {
        contract:
          "Effect-facing application observations must expose readonly Atoms or cold Streams, not listener/unlisten contracts or custom listener storage.",
        types: "Effect subscription policy requires the repository TypeScript program.",
      },
    },
    create(context) {
      const services = context.sourceCode.parserServices;
      if (services.program === undefined || services.program === null)
        throw new Error("Effect subscription policy requires the repository TypeScript program.");
      const checker = services.program.getTypeChecker();
      const owners = new Map();
      function isEffectOwner(file) {
        if (!owners.has(file)) owners.set(file, effectOwner(file, checker));
        return owners.get(file);
      }
      return {
        "Program:exit"(node) {
          const file = services.esTreeNodeToTSNodeMap.get(node);
          const module = checker.getSymbolAtLocation(file);
          if (module === undefined) return;
          for (const exported of checker.getExportsOfModule(module)) {
            const symbol =
              exported.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(exported) : exported;
            const declaration = symbol.declarations?.[0];
            if (declaration === undefined || !isEffectOwner(declaration.getSourceFile())) continue;
            const type =
              symbol.flags & ts.SymbolFlags.Type
                ? checker.getDeclaredTypeOfSymbol(symbol)
                : checker.getTypeOfSymbolAtLocation(symbol, declaration);
            if (subscriptionContract(type, checker)) {
              const local = symbol.declarations.find((entry) => entry.getSourceFile() === file);
              context.report({
                node: local === undefined ? node : (services.tsNodeToESTreeNodeMap.get(local) ?? node),
                messageId: "contract",
              });
            }
          }
          if (!isEffectOwner(file)) return;
          function visit(child) {
            if (ts.isNewExpression(child)) {
              const type = checker.getTypeAtLocation(child);
              const symbol = type.getSymbol();
              if (["Set", "Map", "WeakSet", "WeakMap"].includes(symbol?.name)) {
                const arguments_ = checker.getTypeArguments(type);
                if (arguments_.some((argument) => argument.getCallSignatures().length > 0)) {
                  context.report({ node: services.tsNodeToESTreeNodeMap.get(child), messageId: "contract" });
                }
              }
            }
            ts.forEachChild(child, visit);
          }
          visit(file);
        },
      };
    },
  },
  "no-unowned-effect": {
    meta: {
      type: "problem",
      schema: [],
      messages: {
        unowned:
          "Use standard owned Atom React/Vitest bindings and structured child work; first-party root or detached Effect execution is forbidden.",
        namespace:
          "Effect namespaces must use statically resolved members; dynamic access or forwarding the namespace can expose unowned execution.",
      },
    },
    create(context) {
      const source = context.sourceCode;
      function check(node) {
        if (isForbidden(originOf(node, source))) context.report({ node, messageId: "unowned" });
      }
      return {
        ImportExpression(node) {
          if (libraryModule(node.source.value) !== null) context.report({ node, messageId: "namespace" });
        },
        ImportSpecifier(node) {
          check(node.local);
        },
        ImportDeclaration(node) {
          const module = libraryModule(node.source.value);
          if (module?.split("/").includes("internal")) context.report({ node, messageId: "unowned" });
        },
        ExportAllDeclaration(node) {
          if (libraryModule(node.source.value) !== null) context.report({ node, messageId: "namespace" });
        },
        ExportNamedDeclaration(node) {
          if (node.source === null) return;
          const module = libraryModule(node.source.value);
          if (module === null) return;
          for (const specifier of node.specifiers) {
            const origin = member({ module, path: [] }, specifier.local.name ?? specifier.local.value);
            if (isForbidden(origin) || origin.path.length === 0)
              context.report({ node: specifier, messageId: "unowned" });
          }
        },
        VariableDeclarator(node) {
          for (const variable of source.getDeclaredVariables(node)) {
            for (const id of variable.identifiers) check(id);
          }
          if (node.id.type === "ObjectPattern" && effectOrigin(originOf(node.init, source))) {
            for (const property of node.id.properties) {
              if (property.type === "RestElement" || property.computed)
                context.report({ node: property, messageId: "namespace" });
            }
          }
          const origin = originOf(node.init, source);
          if (
            effectOrigin(origin) &&
            origin.path.length === 0 &&
            node.parent.parent.type === "ExportNamedDeclaration"
          ) {
            context.report({ node, messageId: "namespace" });
          }
        },
        MemberExpression(node) {
          const origin = originOf(node.object, source);
          if (!effectOrigin(origin)) return;
          if (staticMember(node) === null) context.report({ node, messageId: "namespace" });
          else check(node);
        },
        "Program:exit"() {
          for (const scope of source.scopeManager.scopes) {
            for (const reference of scope.references) {
              if (!reference.isValueReference || !reference.isRead()) continue;
              const origin = originOf(reference.identifier, source);
              if (
                effectOrigin(origin) &&
                origin.path.length === 0 &&
                namespaceEscapes(reference.identifier)
              ) {
                context.report({ node: reference.identifier, messageId: "namespace" });
              }
            }
          }
        },
      };
    },
  },
};

function effectOwner(file, checker) {
  if (
    file.statements.some(
      (statement) =>
        ts.isImportDeclaration(statement) &&
        ts.isStringLiteral(statement.moduleSpecifier) &&
        libraryModule(statement.moduleSpecifier.text) !== null,
    )
  )
    return true;
  const module = checker.getSymbolAtLocation(file);
  if (module === undefined) return false;
  return checker.getExportsOfModule(module).some((symbol) => {
    const declaration = symbol.valueDeclaration;
    if (declaration === undefined || declaration.getSourceFile() !== file) return false;
    return applicationTypeMatches(
      checker.getTypeOfSymbolAtLocation(symbol, declaration),
      checker,
      (type) =>
        type.getSymbol()?.declarations?.some((entry) => {
          const parts = entry.getSourceFile().fileName.split("/");
          return parts.includes("node_modules") && parts.includes("effect");
        }) === true,
    );
  });
}

function libraryType(type) {
  const declarations = type.getSymbol()?.declarations;
  return (
    declarations?.some((declaration) =>
      declaration.getSourceFile().fileName.split("/").includes("node_modules"),
    ) === true
  );
}

function callable(type) {
  return type.getCallSignatures().length > 0 || (type.isUnionOrIntersection() && type.types.some(callable));
}

function unlistenHandle(type, checker) {
  const promised = checker.getPromisedTypeOfPromise(type);
  if (promised !== undefined) return unlistenHandle(promised, checker);
  return type.getCallSignatures().some((signature) => signature.parameters.length === 0);
}

function subscriptionContract(type, checker) {
  return applicationTypeMatches(
    type,
    checker,
    (candidate) =>
      !libraryType(candidate) &&
      candidate
        .getCallSignatures()
        .some(
          (signature) =>
            signature.parameters.some((parameter) =>
              callable(checker.getTypeOfSymbol(parameter)),
            ) && unlistenHandle(checker.getReturnTypeOfSignature(signature), checker),
        ),
  );
}

function applicationTypeMatches(type, checker, predicate, seen = new Set()) {
  if (seen.has(type)) return false;
  seen.add(type);
  if (predicate(type)) return true;
  if (libraryType(type)) return false;
  if (type.isUnionOrIntersection())
    return type.types.some((item) => applicationTypeMatches(item, checker, predicate, seen));
  if (
    type
      .getCallSignatures()
      .some((signature) =>
        applicationTypeMatches(checker.getReturnTypeOfSignature(signature), checker, predicate, seen),
      )
  )
    return true;
  return type.getProperties().some((property) => {
    const declaration = property.valueDeclaration ?? property.declarations?.[0];
    return (
      declaration !== undefined &&
      applicationTypeMatches(
        checker.getTypeOfSymbolAtLocation(property, declaration),
        checker,
        predicate,
        seen,
      )
    );
  });
}
