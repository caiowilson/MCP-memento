<?php

declare(strict_types=1);

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

final class Report extends Model
{
    public function owner(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
