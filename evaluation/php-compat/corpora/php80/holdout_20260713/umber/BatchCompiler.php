<?php
declare(strict_types=1);

namespace Holdout\Php80\Umber;

final class BatchCompiler
{
    /**
     * @param array<int, string> $rows
     */
    public function compileShard(array $rows): array
    {
        return array_map(static fn (string $row): string => trim($row), $rows);
    }
}
