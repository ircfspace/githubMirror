# Build stage
FROM python:3.9-alpine AS builder

WORKDIR /app

# Copy requirements file
COPY requirements.txt .

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Final stage
FROM python:3.9-alpine

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy requirements and install
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy source code
COPY bot.py .
COPY config.json .

# Command to run
CMD ["python", "bot.py"]
