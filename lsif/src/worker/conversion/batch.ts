import * as path from 'path'

interface Node {
    segment: string
    children: Node[]
}

export function createBatcher(root: string, documentPaths: string[]): Generator<string[], void, string[]> {
    return traverse(createTree(root, documentPaths))
}

function createTree(root: string, documentPaths: string[]): Node {
    const dirs = Array.from(
        new Set(documentPaths.map(documentPath => dirnameWithoutDot(path.join(root, documentPath))))
    ).filter(dirname => !dirname.startsWith('..'))
    dirs.sort()

    const rootNode: Node = { segment: '', children: [] }

    for (const dir of dirs) {
        if (dir === '') {
            continue
        }

        let node = rootNode
        for (const segment of dir.split('/')) {
            let child = node.children.find(n => n.segment === segment)
            if (!child) {
                child = { segment, children: [] }
                node.children.push(child)
            }

            node = child
        }
    }

    return rootNode
}

function* traverse(root: Node): Generator<string[], void, string[]> {
    let frontier: [string, Node[]][] = [['', root.children]]

    while (frontier.length > 0) {
        const exists = yield frontier.map(([parent]) => parent)

        frontier = frontier
            .filter(([parent]) => exists.includes(parent))
            .flatMap(([parent, children]) =>
                children.map((child): [string, Node[]] => [path.join(parent, child.segment), child.children])
            )
    }
}

/**
 * Return the dirname of the given path. Returns empty string
 * if the path denotes a file in the current directory.
 */
export function dirnameWithoutDot(pathname: string): string {
    // TODO - move this function
    return path.dirname(pathname) === '.' ? '' : path.dirname(pathname)
}
