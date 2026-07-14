<?php
declare(strict_types=1);

namespace Holdout\Php80\Umber;

use Closure;

final class CompilerPipeline
{
    /** @var Closure */
    private $step;

    public function __construct(BatchCompiler $compiler)
    {
        $this->step = Closure::fromCallable([$compiler, 'compileShard']);
    }

    /** @param array<int, string> $rows */
    public function run(array $rows): array
    {
        return ($this->step)($rows);
    }
}
